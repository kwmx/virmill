//go:build linux && amd64

package creating

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"syscall"
	"virmill.local/core/internal/app/provision"
	"virmill.local/core/internal/backend/seed"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
)

type SeedTool interface {
	Identity(context.Context) (seed.Identity, error)
	Build(context.Context, string, map[string][]byte) (seed.Artifact, error)
}
type seedCacheIdentity struct {
	Device uint64 `json:"device,string"`
	Inode  uint64 `json:"inode,string"`
}
type seedRecipe struct {
	Config         provision.Config  `json:"config"`
	Tool           seed.Identity     `json:"tool"`
	Artifact       seed.Artifact     `json:"artifact"`
	CacheDirectory string            `json:"cacheDirectory"`
	CacheIdentity  seedCacheIdentity `json:"cacheIdentity"`
}

func seedCache(path string) (*os.File, *os.Root, seedCacheIdentity, error) {
	var id seedCacheIdentity
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, id, domain.Fail("INVALID_INPUT", "canonical private seed cache required")
	}
	if err := platform.PrivateDir(path); err != nil {
		return nil, nil, id, err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, nil, id, err
	}
	f := os.NewFile(uintptr(fd), path)
	var space unix.Statfs_t
	if err = unix.Fstatfs(fd, &space); err != nil {
		f.Close()
		return nil, nil, id, err
	}
	required := uint64(seed.MaxISOBytes + 3*seed.MaxContentBytes + (1 << 20))
	if space.Bsize <= 0 || space.Bavail < (required+uint64(space.Bsize)-1)/uint64(space.Bsize) {
		f.Close()
		return nil, nil, id, domain.Fail("INSUFFICIENT_SPACE", "private seed cache lacks the bounded generator/readback space budget")
	}

	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, id, err
	}
	native, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		f.Close()
		return nil, nil, id, errors.New("native seed cache identity unavailable")
	}
	id = seedCacheIdentity{Device: uint64(native.Dev), Inode: native.Ino}
	root, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		f.Close()
		return nil, nil, id, err
	}
	return f, root, id, nil
}
func checkCloudSource(config provision.Config, spec domain.CreationSpec, artifact importing.Artifact) error {
	if artifact.Kind != "PreparedDiskSet" {
		return domain.Fail("INVALID_INPUT", "NoCloud creation requires independently prepared cloud-image disks; installation ISO and OVF sources use their own workflows")
	}
	matched := false
	for _, disk := range artifact.Disks {
		if disk.SourceID != config.SourceDiskID {
			continue
		}
		for _, source := range artifact.SourceFiles {
			if source.Path == disk.SourcePath && source.SHA256 == config.SourceSHA256 {
				matched = true
			}
		}
	}
	boot := false
	for _, disk := range spec.Disks {
		if disk.SourceID == config.SourceDiskID && disk.BootOrder == 1 {
			boot = true
		}
	}
	if !matched || !boot {
		return domain.Fail("SOURCE_CHANGED", "declared cloud source digest must match its prepared root source file and the first boot disk")
	}
	media := false
	for _, m := range spec.Media {
		if m.SourceID == config.MediaID && m.BootOrder == 0 {
			media = true
		}
	}
	if !media {
		return domain.Fail("INVALID_INPUT", "map the NoCloud medium explicitly with bootOrder zero")
	}
	return provision.Validate(config, spec)
}
func memberDigests(files map[string][]byte) map[string]string {
	out := map[string]string{}
	for name, b := range files {
		h := sha256.Sum256(b)
		out[name] = hex.EncodeToString(h[:])
	}
	return out
}
func (s *Service) planSeed(ctx context.Context, c *provision.Config, spec domain.CreationSpec, artifact importing.Artifact) (result *seedRecipe, resultErr error) {
	if c == nil {
		return nil, nil
	}
	if s.SeedTool == nil {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "NoCloud seed generator is unavailable")
	}
	if err := checkCloudSource(*c, spec, artifact); err != nil {
		return nil, err
	}
	files, err := provision.Render(*c, spec)
	if err != nil {
		return nil, err
	}
	parent, root, id, err := seedCache(s.SeedCache)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	defer root.Close()
	tool, err := s.SeedTool.Identity(ctx)
	if err != nil {
		return nil, err
	}
	name := "seed-preview-" + domain.ID()
	if err = root.Mkdir(name, 0700); err != nil {
		return nil, err
	}
	defer func() {
		if err := root.RemoveAll(name); err != nil {
			result = nil
			resultErr = fmt.Errorf("private seed preview cleanup failed: %w", err)
		}
	}()
	built, err := s.SeedTool.Build(ctx, filepath.Join(s.SeedCache, name), files)
	if err != nil {
		return nil, err
	}
	if err = validSeedProof(built, files); err != nil {
		return nil, err
	}
	return &seedRecipe{Config: *c, Tool: tool, Artifact: built, CacheDirectory: s.SeedCache, CacheIdentity: id}, nil
}
func validSeedProof(a seed.Artifact, files map[string][]byte) error {
	digest, err := hex.DecodeString(a.SHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != a.SHA256 || a.Path != "seed.iso" || a.FileBytes < 17*2048 || a.FileBytes > seed.MaxISOBytes || a.FileBytes%2048 != 0 || a.Verification != "iso9660-CIDATA+exact-members+content-readback" || !same(a.Members, memberDigests(files)) {
		return domain.Fail("RECOVERY_REQUIRED", "generated NoCloud seed proof differs from the reviewed files")
	}
	return nil
}
func (s *Service) checkSeed(ctx context.Context, in input) error {
	if in.Seed == nil {
		return nil
	}
	if s.SeedTool == nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "NoCloud seed generator unavailable")
	}
	recipe := in.Seed
	if err := checkCloudSource(recipe.Config, in.Target.Spec, in.Artifact); err != nil {
		return err
	}
	files, err := provision.Render(recipe.Config, in.Target.Spec)
	if err != nil {
		return err
	}
	if err = validSeedProof(recipe.Artifact, files); err != nil {
		return err
	}
	tool, err := s.SeedTool.Identity(ctx)
	if err != nil {
		return err
	}
	if tool != recipe.Tool {
		return domain.Fail("SOURCE_CHANGED", "seed generator changed after preview")
	}
	parent, root, id, err := seedCache(recipe.CacheDirectory)
	if err != nil {
		return err
	}
	parent.Close()
	root.Close()
	if id != recipe.CacheIdentity {
		return domain.Fail("STALE_PLAN", "private seed cache changed after preview")
	}
	return nil
}
func seedStage(p domain.Plan) string { return "creation-seed-" + p.ID }

// The durable plan contains all nonsecret seed inputs and exact output hashes.
// Execution reproduces the seed before any managed volume allocation. The ISO is
// uploaded as an ordinary member of the same durable volume set; temporary seed
// files can then be removed independently of partial backend volumes.
func (s *Service) executeSeed(ctx context.Context, p domain.Plan, in input) (*os.Root, func() error, error) {
	noop := func() error { return nil }
	if in.Seed == nil {
		return nil, noop, nil
	}
	parent, root, id, err := seedCache(in.Seed.CacheDirectory)
	if err != nil {
		return nil, noop, err
	}
	closeRoots := func() { root.Close(); parent.Close() }
	if id != in.Seed.CacheIdentity {
		closeRoots()
		return nil, noop, domain.Fail("STALE_PLAN", "private seed cache changed")
	}
	name := seedStage(p)
	if err = root.Mkdir(name, 0700); err != nil {
		closeRoots()
		return nil, noop, err
	}
	cleanup := func() error { err := root.RemoveAll(name); closeRoots(); return err }
	if err = operations.Note(ctx, s.Store, "Intent persisted: reproduce the identity-bound NoCloud seed before volume allocation"); err != nil {
		if e := cleanup(); e != nil {
			return nil, noop, e
		}
		return nil, noop, err
	}
	files, err := provision.Render(in.Seed.Config, in.Target.Spec)
	if err == nil {
		var built seed.Artifact
		err = s.cancellable(ctx, func(worker context.Context) error {
			var e error
			built, e = s.SeedTool.Build(worker, filepath.Join(in.Seed.CacheDirectory, name), files)
			return e
		})
		if err == nil && !same(built, in.Seed.Artifact) {
			err = domain.Fail("SOURCE_CHANGED", "seed output differs from the approved deterministic artifact")
		}
	}
	if err != nil {
		if e := cleanup(); e != nil {
			return nil, noop, e
		}
		if ctx.Err() == nil {
			canceled, e := operations.CancellationRequested(ctx, s.Store)
			if e != nil {
				return nil, noop, e
			}
			if canceled {
				return nil, noop, operations.ErrCanceledSafely
			}
		}
		return nil, noop, err
	}
	generated, err := root.OpenRoot(name)
	if err != nil {
		if e := cleanup(); e != nil {
			return nil, noop, e
		}
		return nil, noop, err
	}
	return generated, func() error { generated.Close(); return cleanup() }, nil
}

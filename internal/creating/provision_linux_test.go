//go:build linux && amd64

package creating

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/provision"
	"virmill.local/core/internal/backend/seed"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
)

// Synthetic ISO bytes exercise coordinator phases only. The real generator test
// below replaces this tool and reports its file-level evidence separately.
type seedPhaseTool struct {
	calls   atomic.Int32
	fail    bool
	changed bool
	started chan struct{}
}

func (s *seedPhaseTool) Identity(context.Context) (seed.Identity, error) {
	version := "synthetic"
	if s.changed {
		version = "changed"
	}
	return seed.Identity{Path: "fixture-only", SHA256: strings.Repeat("a", 64), Version: version}, nil
}
func (s *seedPhaseTool) Build(ctx context.Context, work string, files map[string][]byte) (seed.Artifact, error) {
	count := s.calls.Add(1)
	canonical, err := operations.Canonical(files)
	if err != nil {
		return seed.Artifact{}, err
	}
	h := sha256.Sum256(canonical)
	b := bytes.Repeat(h[:], 40*2048/32)
	if err = os.WriteFile(filepath.Join(work, "seed.iso"), b, 0400); err != nil {
		return seed.Artifact{}, err
	}
	if count == 2 && s.started != nil {
		close(s.started)
		<-ctx.Done()
		return seed.Artifact{}, ctx.Err()
	}
	if count == 2 && s.fail {
		return seed.Artifact{}, errors.New("synthetic seed generation failure")
	}
	return seed.Artifact{Path: "seed.iso", FileBytes: int64(len(b)), SHA256: hashFixture(b), Members: memberDigests(files), Verification: "iso9660-CIDATA+exact-members+content-readback"}, nil
}
func provisioningPublicKey() string {
	var b bytes.Buffer
	for _, field := range [][]byte{[]byte("ssh-ed25519"), bytes.Repeat([]byte{0x42}, 32)} {
		_ = binary.Write(&b, binary.BigEndian, uint32(len(field)))
		b.Write(field)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(b.Bytes())
}
func provisioningFixture(t *testing.T, tool SeedTool) (*Service, *fixtureBackend, app.Request) {
	t.Helper()
	s, backend, r, dir := creationFixture(t)
	a, _, err := s.LoadSource(context.Background(), s.Store, 1000, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.Kind = "PreparedDiskSet"
	a.System.Items = nil
	for i := range a.Disks {
		a.Disks[i].SourcePath = a.Disks[i].SourceID + ".raw"
	}
	sourceHash := strings.Repeat("c", 64)
	a.SourceFiles = []importing.DiskSetFile{{Path: "boot.raw", SHA256: sourceHash}, {Path: "data.raw", SHA256: strings.Repeat("d", 64)}}
	s.LoadSource = func(context.Context, *store.Store, uint32, string) (importing.Artifact, string, error) {
		return a, dir, nil
	}
	s.SeedTool = tool
	s.SeedCache = filepath.Join(t.TempDir(), "private-cache")
	hw := r.Input["hardware"].(map[string]any)
	nics := hw["nics"].([]any)
	for _, nic := range nics {
		nic.(map[string]any)["sourceIndex"] = -1
	}
	hw["media"] = []any{map[string]any{"sourceID": "cloud-init", "bus": "sata", "bootOrder": 0}}
	config := provision.Config{Profile: provision.Profile, CloudInitCompatible: true, SourceDiskID: "boot", SourceSHA256: sourceHash, SourceReference: "https://example.invalid/declared-cloud-image", Hostname: "cloud-fixture", UserName: "operator", AuthorizedKeys: []string{provisioningPublicKey()}, IPv6: "disabled", MediaID: "cloud-init", Interfaces: []provision.Interface{{NICID: "internet", Role: "normal", Mode: "dhcp", DefaultRoute: true, RouteMetric: 100, DNS: []string{}}, {NICID: "lab", Role: "protected", Mode: "none", RouteMetric: 200, DNS: []string{}}}}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	r.Input["provisioning"] = value
	return s, backend, r
}
func TestNoCloudPlanAndExecutionBindExactSeedAndCloneIdentity(t *testing.T) {
	tool := &seedPhaseTool{}
	s, backend, r := provisioningFixture(t, tool)
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls.Load() != 1 || backend.allocated != 0 {
		t.Fatal("preview mutated managed volumes or missed seed proof")
	}
	_, encoded, err := s.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var recipe input
	if err = json.Unmarshal(encoded, &recipe); err != nil {
		t.Fatal(err)
	}
	if recipe.Seed == nil || len(recipe.Volumes) != 3 || recipe.Volumes[2].SHA256 != recipe.Seed.Artifact.SHA256 || recipe.Volumes[2].ContentType != "cdrom-iso" {
		t.Fatal("seed missing from durable volume set", recipe)
	}
	entries, err := os.ReadDir(s.SeedCache)
	if err != nil || len(entries) != 0 {
		t.Fatal("preview content retained", entries, err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "succeeded" || tool.calls.Load() != 2 || backend.allocated != 3 || backend.populated != 3 || backend.definitions != 1 {
		t.Fatal(j, tool.calls.Load())
	}
	if _, err = os.Stat(filepath.Join(s.SeedCache, seedStage(p))); !os.IsNotExist(err) {
		t.Fatal("temporary seed not cleaned", err)
	}
	receipt, err := s.load(p.ID)
	if err != nil || !receipt.VolumesVerified || receipt.GuestBootVerified {
		t.Fatal(receipt, err)
	}
	if hashFixture(backend.volumes[recipe.Volumes[2].Name]) != recipe.Seed.Artifact.SHA256 {
		t.Fatal("uploaded seed differs from preview")
	}
	// Another reviewed clone gets independent IDs and therefore different seed bytes.
	backend.mu.Lock()
	backend.defined = false
	backend.mu.Unlock()
	second, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	_, otherBytes, err := s.Store.Plan(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	var other input
	if err = json.Unmarshal(otherBytes, &other); err != nil {
		t.Fatal(err)
	}
	if recipe.Target.Spec.UUID == other.Target.Spec.UUID || recipe.Seed.Artifact.SHA256 == other.Seed.Artifact.SHA256 || recipe.Target.Spec.NICs[0].MAC == other.Target.Spec.NICs[0].MAC {
		t.Fatal("clone reused NoCloud identity")
	}
}
func TestNoCloudFailureAndCancellationPrecedeAllManagedAllocations(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancel"}[cancel], func(t *testing.T) {
			tool := &seedPhaseTool{fail: !cancel}
			if cancel {
				tool.started = make(chan struct{})
			}
			s, backend, r := provisioningFixture(t, tool)
			p, err := s.Plan(context.Background(), 1000, r)
			if err != nil {
				t.Fatal(err)
			}
			j := applyCreation(t, s, p)
			if cancel {
				select {
				case <-tool.started:
				case <-time.After(5 * time.Second):
					t.Fatal("seed worker did not start")
				}
				if _, err = s.Engine.Cancel(j.ID); err != nil {
					t.Fatal(err)
				}
			}
			j = awaitCreation(t, s, j.ID)
			want := "recovery-required"
			if cancel {
				want = "canceled"
			}
			if j.State != want || backend.allocated != 0 || backend.definitions != 0 {
				t.Fatal("seed failed after host effect or wrong result", j)
			}
			if _, err = os.Stat(filepath.Join(s.SeedCache, seedStage(p))); !os.IsNotExist(err) {
				t.Fatal("failed seed stage remains", err)
			}
		})
	}
}
func TestNoCloudSourceToolAndCacheChangesFailClosed(t *testing.T) {
	for _, mode := range []string{"source-digest", "media-boot", "root-user", "password", "tool", "cache"} {
		t.Run(mode, func(t *testing.T) {
			tool := &seedPhaseTool{}
			s, backend, r := provisioningFixture(t, tool)
			config := r.Input["provisioning"].(map[string]any)
			switch mode {
			case "source-digest":
				config["sourceSHA256"] = strings.Repeat("e", 64)
			case "media-boot":
				r.Input["hardware"].(map[string]any)["media"].([]any)[0].(map[string]any)["bootOrder"] = 3
			case "root-user":
				config["userName"] = "root"
			case "password":
				config["password"] = "must-never-enter-journal"
			}
			p, err := s.Plan(context.Background(), 1000, r)
			if mode != "tool" && mode != "cache" {
				if err == nil {
					t.Fatal("unsafe declaration accepted", mode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "tool" {
				tool.changed = true
			} else {
				if err = os.Rename(s.SeedCache, s.SeedCache+"-before"); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(s.SeedCache, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: p.Acknowledgements}); err == nil {
				t.Fatal("stale seed plan accepted")
			}
			if backend.allocated != 0 {
				t.Fatal("stale plan allocated volumes")
			}
		})
	}
}
func TestVerifiedNoCloudMediaResumesWithoutRegenerating(t *testing.T) {
	tool := &seedPhaseTool{}
	s, backend, r := provisioningFixture(t, tool)
	backend.fail = "define"
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	s.SeedTool = nil // Recovery consumes already verified media, never regenerates a seed.
	if err = os.RemoveAll(s.SeedCache); err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.fail = ""
	backend.mu.Unlock()
	resume, err := s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: j.ID})
	if err != nil {
		t.Fatal(err)
	}
	child := awaitCreation(t, s, applyCreation(t, s, resume).ID)
	if child.State != "succeeded" || tool.calls.Load() != 2 || backend.allocated != 3 || backend.populated != 3 {
		t.Fatal("recovery replayed seed or storage effects", child, tool.calls.Load())
	}
}
func TestRealNoCloudGeneratorThroughCreationCoordinator(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("enable actual generated ISO fixture explicitly")
	}
	s, backend, r := provisioningFixture(t, seed.Tool{})
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	receipt, err := s.load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	media := backend.volumes[receipt.Volumes[2].Intent.Name]
	if string(media[16*2048+40:16*2048+46]) != "CIDATA" {
		t.Fatal("real seed missing CIDATA label")
	}
	h := sha256.Sum256(media)
	t.Logf("actual confined xorriso preview/execution/readback, synthetic storage backend only; seed SHA-256 %s; no native volume stream, cloud-init runtime or guest boot", hex.EncodeToString(h[:]))
}

func TestDocumentedNoCloudCreationSchema(t *testing.T) {
	b, err := os.ReadFile("../../examples/creation/nocloud.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = validation.Schema("vm-creation-input", b); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedNoCloudSecretValuesNeverReachJournalOrErrors(t *testing.T) {
	for _, field := range []string{"password", "authorizedKeys", "sourceReference"} {
		tool := &seedPhaseTool{}
		s, _, r := provisioningFixture(t, tool)
		config := r.Input["provisioning"].(map[string]any)
		secret := "virmill-secret-sentinel-do-not-log"
		switch field {
		case "password":
			config[field] = secret
		case "authorizedKeys":
			config[field] = []string{"-----BEGIN OPENSSH PRIVATE KEY-----" + secret}
		case "sourceReference":
			config[field] = "https://user:" + secret + "@example.invalid/image"
		}
		_, err := s.Plan(context.Background(), 1000, r)
		if err == nil {
			t.Fatal("secret accepted")
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatal("secret entered error text")
		}
		if tool.calls.Load() != 0 {
			t.Fatal("secret reached seed generator")
		}
		records, err := s.Store.MetadataRecords()
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("secret entered journal metadata")
		}
	}
}

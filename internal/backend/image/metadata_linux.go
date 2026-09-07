//go:build linux && amd64

package image

import (
	"context"
	"fmt"
	"os"
	"time"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/wire"
)

// InspectFile reads only the chosen inactive image's metadata. Backing filenames
// are declarations, not verified dependencies: the graph adapter must resolve
// them independently. No sibling file or backing file is mounted in the worker.
// Callers must establish that the image is inactive before invoking this method.
func (t Tool) InspectFile(ctx context.Context, source *os.File, workspace, format string) (Info, fileidentity.Identity, error) {
	var info Info
	var empty fileidentity.Identity
	if source == nil || (format != "raw" && format != "qcow2") {
		return info, empty, domain.Fail("UNSUPPORTED_CAPABILITY", "single-file dependency inspection supports explicit raw or qcow2 only")
	}
	before, err := fileidentity.InspectFile(source, false)
	if err != nil {
		return info, empty, err
	}
	if _, err = t.Identity(ctx); err != nil {
		return info, before, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case workerSlots <- struct{}{}:
	case <-ctx.Done():
		return info, before, ctx.Err()
	}
	defer func() { <-workerSlots }()
	// Do not add --backing-chain or -U: neither following unapproved paths nor
	// ignoring an active writer's lock is allowed in this metadata-only adapter.
	cmd, cleanup, err := platform.ConfinedDiskFileCommand(ctx, source, workspace, []string{"info", "--output=json", "-f", format, "/source/image"})
	if err != nil {
		return info, before, err
	}
	defer cleanup()
	out, diagnostic := &boundedBuffer{limit: 1 << 20}, &boundedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = out, diagnostic, 2*time.Second
	if err = cmd.Run(); err != nil {
		return info, before, fmt.Errorf("confined image metadata unavailable: %w: %s", err, diagnostic.Bytes())
	}
	if err = wire.Decode(out.Bytes(), &info); err != nil {
		return info, before, err
	}
	after, err := fileidentity.InspectFile(source, false)
	if err != nil {
		return info, before, err
	}
	if before != after {
		return info, before, domain.Fail("SOURCE_CHANGED", "image changed while its dependency metadata was inspected")
	}
	if err = checkFileInfo(info, format); err != nil {
		return info, before, err
	}
	return info, before, nil
}

func checkFileInfo(info Info, format string) error {
	if info.Filename != "/source/image" || info.Format != format || info.VirtualSize < 1 || info.Encrypted || info.Dirty {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "image identity, encryption, size or dirty state prevents dependency proof")
	}
	// Reuse the strict single-node validator, removing only the declared backing
	// edge which the caller must resolve against the native storage graph.
	node := info
	node.Backing, node.FullBacking, node.BackingFormat = "", "", ""
	return CheckChain([]Info{node}, format, "image", info.VirtualSize, map[string]bool{"image": true})
}

//go:build linux && cgo

package libvirt

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func (p *Provider) ColdRuntimeVersions(ctx context.Context, uri string, tpm bool) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	lv, err := c.GetLibVersion()
	if err != nil {
		return nil, err
	}
	qv, err := c.GetVersion()
	if err != nil {
		return nil, err
	}
	version := func(v uint32) string { return fmt.Sprintf("%d.%d.%d", v/1000000, v/1000%1000, v%1000) }
	versions := map[string]string{"libvirt": version(lv), "qemu": version(qv)}
	emulatorHash, err := firmwareFileDigest("/usr/bin/qemu-system-x86_64")
	if err != nil {
		return nil, err
	}
	versions["qemu-emulator-sha256"] = emulatorHash
	if tpm {
		// A fixed system executable supplies its actual observed version. This
		// does not infer a producer lock feature from a version number.
		f, err := openSystemFile("/usr/bin/swtpm")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		if _, err = firmwareFileDigest("/usr/bin/swtpm"); err != nil {
			return nil, err
		}
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(bounded, "/usr/bin/swtpm", "--version")
		cmd.Env = []string{"PATH=/usr/bin", "LC_ALL=C"}
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		versions["swtpm"] = strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	}
	return versions, ctx.Err()
}

//go:build linux && cgo

package libvirt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

type probeBuffer struct{ bytes.Buffer }

func (b *probeBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New("device probe output exceeds bound")
	}
	return b.Buffer.Write(p)
}
func probeCreationDevices(ctx context.Context, emulator string, s domain.CreationSpec) (string, error) {
	if !strings.HasPrefix(s.Machine, "pc-q35-") && !strings.HasPrefix(s.Machine, "pc-i440fx-") {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "creation mapping currently requires an advertised versioned Q35 or i440fx PC machine")
	}
	digest, err := firmwareFileDigest(emulator)
	if err != nil {
		return "", err
	}
	models := map[string]bool{}
	if p := s.DevicePolicy; p != nil {
		if err := p.Validate(s.Machine); err != nil {
			return "", err
		}
		if p.USBController == "qemu-xhci" {
			models["qemu-xhci"] = true
		}
		if p.MemoryBalloon == "virtio" {
			models["virtio-balloon-pci"] = true
		}
		if p.Chipset == "q35" {
			models["pcie-root-port"] = true
		}
	}
	for _, n := range s.NICs {
		model := map[string]string{"virtio": "virtio-net-pci", "e1000e": "e1000e", "rtl8139": "rtl8139"}[n.Model]
		if model == "" {
			return "", errors.New("unknown NIC probe")
		}
		models[model] = true
	}
	if s.Graphics == "vnc-unix" {
		models["VGA"] = true
	}
	for _, d := range s.Disks {
		if d.Bus == "scsi" {
			models["virtio-scsi-pci"] = true
		}
	}
	for _, m := range s.Media {
		switch m.Bus {
		case "sata":
			models["ide-cd"] = true
		case "scsi":
			models["virtio-scsi-pci"] = true
			models["scsi-cd"] = true
		default:
			return "", errors.New("unknown media bus probe")
		}
	}
	names := []string{}
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	proof := []string{digest}
	for _, model := range names {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		cmd := exec.CommandContext(bounded, emulator, "-no-user-config", "-nodefaults", "-machine", "none", "-display", "none", "-device", model+",help")
		cmd.Env = []string{"PATH=/usr/bin", "LC_ALL=C"}
		var output probeBuffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		cmd.WaitDelay = time.Second
		err = cmd.Run()
		cancel()
		if err != nil || !strings.HasPrefix(output.String(), model+" options:") {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "trusted emulator does not advertise requested device "+model)
		}
		proof = append(proof, inventoryDigest(output.String()))
	}
	return inventoryDigest(proof), nil
}

type firmwareDescriptor struct {
	Mapping struct {
		Device     string `json:"device"`
		Mode       string `json:"mode"`
		Executable struct {
			Filename string `json:"filename"`
			Format   string `json:"format"`
		} `json:"executable"`
		Template struct {
			Filename string `json:"filename"`
			Format   string `json:"format"`
		} `json:"nvram-template"`
	} `json:"mapping"`
	Targets []struct {
		Architecture string   `json:"architecture"`
		Machines     []string `json:"machines"`
	} `json:"targets"`
	Features []string `json:"features"`
}

func descriptorMatches(d firmwareDescriptor, s domain.CreationSpec) bool {
	f := s.Firmware
	m := d.Mapping
	if m.Device != "flash" || (m.Mode != "" && m.Mode != "split") || m.Executable.Filename != f.Code || m.Template.Filename != f.Template || m.Executable.Format != f.Format || m.Template.Format != f.Format {
		return false
	}
	target := false
	for _, t := range d.Targets {
		if t.Architecture != s.Architecture {
			continue
		}
		for _, pattern := range t.Machines {
			match, err := filepath.Match(pattern, s.Machine)
			if err == nil && match {
				target = true
			}
		}
	}
	if !target {
		return false
	}
	features := map[string]bool{}
	for _, feature := range d.Features {
		features[feature] = true
	}
	if f.SecureBoot {
		return features["secure-boot"] && features["enrolled-keys"] && features["requires-smm"]
	}
	return !features["enrolled-keys"] && !features["requires-smm"]
}
func matchingFirmwareDescriptor(s domain.CreationSpec) (string, error) {
	paths, err := filepath.Glob("/usr/share/qemu/firmware/*.json")
	if err != nil {
		return "", err
	}
	if len(paths) > 256 {
		return "", errors.New("firmware descriptor inventory exceeds bound")
	}
	for _, path := range paths {
		f, e := openSystemFile(path)
		if e != nil {
			return "", e
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			return "", e
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.Mode().IsRegular() || owner.Uid != 0 || st.Mode().Perm()&0022 != 0 || st.Size() > 64<<10 {
			f.Close()
			return "", domain.Fail("PERMISSION_DENIED", "untrusted system firmware descriptor")
		}
		b, e := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		f.Close()
		if e != nil {
			return "", e
		}
		if e = wire.Validate(b); e != nil {
			return "", e
		}
		var d firmwareDescriptor
		if e = json.Unmarshal(b, &d); e != nil {
			return "", e
		}
		if descriptorMatches(d, s) {
			return inventoryDigest(string(b)), nil
		}
	}
	return "", domain.Fail("UNSUPPORTED_CAPABILITY", "no trusted system descriptor matches the chosen firmware/template, machine and Secure Boot key policy")
}

package importer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func manifestFixture(t *testing.T, algorithm, spacing string, change func(string) string) []byte {
	t.Helper()
	files := []struct{ name, content string }{{"appliance.ovf", descriptor}, {"boot.vmdk", "synthetic-boot"}, {"data.vmdk", "synthetic-data"}, {"appliance.nvram", "synthetic-firmware-state"}}
	var manifest strings.Builder
	for _, file := range files {
		var digest string
		switch algorithm {
		case "SHA1":
			digest = fmt.Sprintf("%x", sha1.Sum([]byte(file.content)))
		case "SHA256":
			digest = fmt.Sprintf("%x", sha256.Sum256([]byte(file.content)))
		case "SHA512":
			digest = fmt.Sprintf("%x", sha512.Sum512([]byte(file.content)))
		default:
			t.Fatal("unsupported fixture algorithm")
		}
		fmt.Fprintf(&manifest, "%s%s(%s) = %s\r\n", algorithm, spacing, file.name, digest)
	}
	text := manifest.String()
	if change != nil {
		text = change(text)
	}
	files = append(files, struct{ name, content string }{"appliance.mf", text})
	var out bytes.Buffer
	w := tar.NewWriter(&out)
	for _, file := range files {
		if err := w.WriteHeader(&tar.Header{Name: file.name, Size: int64(len(file.content)), Mode: 0600}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(file.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestManifestHorizontalSpacingAndUnterminatedTar(t *testing.T) {
	for _, algorithm := range []string{"SHA1", "SHA256", "SHA512"} {
		for _, spacing := range []string{"", " ", "\t", "  \t"} {
			for _, endMarkers := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/spacing-%q/end-%t", algorithm, spacing, endMarkers), func(t *testing.T) {
					archive := manifestFixture(t, algorithm, spacing, nil)
					if !endMarkers {
						// The owner-supplied VirtualBox OVA ends exactly after the
						// final member's padding. No member content is removed.
						archive = archive[:len(archive)-1024]
					}
					report, err := InspectTar(context.Background(), bytes.NewReader(archive), DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					if report.Integrity != "provided-checksums-valid; publisher-unverified" || report.Readiness != "inspection-only" || len(report.Disks) != 2 || len(report.Members) != 5 {
						t.Fatalf("lost content or incorrect integrity/readiness: %+v", report)
					}
					if slices.Contains(report.Warnings, "WEAK_CHECKSUM_SHA1") != (algorithm == "SHA1") {
						t.Fatal("weak algorithm warning does not match manifest")
					}
					if report.SHA256 != fmt.Sprintf("%x", sha256.Sum256(archive)) {
						t.Fatal("source digest omits bytes")
					}
				})
			}
		}
	}
}

func TestSpacedManifestStillRejectsInvalidChecksums(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"wrong-digest-length": func(s string) string { return strings.Replace(s, " = ", " = 0", 1) },
		"wrong-digest-value": func(s string) string {
			i := strings.Index(s, " = ") + 3
			replacement := byte('0')
			if s[i] == replacement {
				replacement = '1'
			}
			return s[:i] + string(replacement) + s[i+1:]
		},
		"missing-member":        func(s string) string { return strings.Replace(s, "boot.vmdk", "absent.vmdk", 1) },
		"unsafe-path":           func(s string) string { return strings.Replace(s, "boot.vmdk", "../boot.vmdk", 1) },
		"unsupported-algorithm": func(s string) string { return strings.ReplaceAll(s, "SHA256", "MD5") },
		"vertical-spacing":      func(s string) string { return strings.Replace(s, "SHA256 (", "SHA256\n(", 1) },
		"trailing-garbage":      func(s string) string { return strings.Replace(s, "\r\n", " unexpected\r\n", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectTar(context.Background(), bytes.NewReader(manifestFixture(t, "SHA256", " ", change)), DefaultLimits()); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

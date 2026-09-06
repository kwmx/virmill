// Package plugins verifies versioned signed payloads before any plugin execution.
package plugins

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const PackageLimit = 64 << 20

type File struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable"`
}
type Index struct {
	FormatVersion string `json:"formatVersion"`
	PluginID      string `json:"pluginID"`
	PluginVersion string `json:"pluginVersion"`
	SigningKeyID  string `json:"signingKeyID"`
	Files         []File `json:"files"`
}
type Manifest struct {
	ManifestVersion string `json:"manifestVersion"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Protocol        struct {
		MinVersion string `json:"minVersion"`
		MaxVersion string `json:"maxVersion"`
		Transport  string `json:"transport"`
	} `json:"protocol"`
	Entrypoints map[string]struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256,omitempty"`
	} `json:"entrypoints"`
	ExtensionTypes []string          `json:"extensionTypes"`
	Permissions    []Permission      `json:"permissions"`
	Network        string            `json:"network"`
	UI             []json.RawMessage `json:"ui,omitempty"`
	ConfigSchema   json.RawMessage   `json:"configSchema,omitempty"`
	Extensions     map[string]any    `json:"extensions,omitempty"`
}
type Permission struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}
type Verified struct {
	Index    Index    `json:"index"`
	Manifest Manifest `json:"manifest"`
	Digest   string   `json:"packageDigest"`
	files    map[string][]byte
}

var portable = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

func packagePath(path string) error {
	if !portable.MatchString(path) {
		return errors.New("non-portable package path")
	}
	return importer.SafePath(path)
}
func ValidateManifest(b []byte) (Manifest, error) {
	var m Manifest
	if e := validation.Schema("plugin-manifest", b); e != nil {
		return m, e
	}
	if e := wire.Decode(b, &m); e != nil {
		return m, e
	}
	for _, entry := range m.Entrypoints {
		if e := packagePath(entry.Path); e != nil {
			return m, e
		}
	}
	return m, nil
}
func hashBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func Pack(root string, key ed25519.PrivateKey, keyID string, w io.Writer) (string, error) {
	if len(key) != ed25519.PrivateKeySize || keyID == "" {
		return "", errors.New("actual Ed25519 key and signing-key ID required")
	}
	files := map[string][]byte{}
	index := Index{FormatVersion: "1", SigningKeyID: keyID, Files: []File{}}
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path == root {
			return nil
		}
		name, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		name = filepath.ToSlash(name)
		if e = packagePath(name); e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("package symlinks forbidden")
		}
		if d.IsDir() {
			return nil
		}
		if name == "package-index.json" || name == "package-index.sig" {
			return errors.New("packer generates the inventory/signature; remove stale ones")
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return errors.New("package special entry forbidden")
		}
		if st.Size() > PackageLimit-total || len(files) >= 10000 {
			return errors.New("package exceeds bounds")
		}
		total += st.Size()
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if int64(len(b)) != st.Size() {
			return errors.New("package source changed")
		}
		files[name] = b
		index.Files = append(index.Files, File{Path: name, Size: int64(len(b)), SHA256: hashBytes(b), Executable: st.Mode().Perm()&0111 != 0})
		return nil
	})
	if err != nil {
		return "", err
	}
	m, e := ValidateManifest(files["manifest.json"])
	if e != nil {
		return "", e
	}
	index.PluginID = m.ID
	index.PluginVersion = m.Version
	sort.Slice(index.Files, func(i, j int) bool { return index.Files[i].Path < index.Files[j].Path })
	for _, entry := range m.Entrypoints {
		b, ok := files[entry.Path]
		if !ok || entry.SHA256 == "" || entry.SHA256 != hashBytes(b) {
			return "", errors.New("distribution entrypoint requires its actual SHA-256")
		}
	}
	canonical, e := operations.Canonical(index)
	if e != nil {
		return "", e
	}
	signature := ed25519.Sign(key, canonical)
	tw := tar.NewWriter(w)
	write := func(name string, b []byte, executable bool) error {
		mode := int64(0644)
		if executable {
			mode = 0755
		}
		if e := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(b)), Mode: mode, Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}); e != nil {
			return e
		}
		_, e := tw.Write(b)
		return e
	}
	for _, f := range index.Files {
		if e = write(f.Path, files[f.Path], f.Executable); e != nil {
			return "", e
		}
	}
	if e = write("package-index.json", canonical, false); e != nil {
		return "", e
	}
	if e = write("package-index.sig", signature, false); e != nil {
		return "", e
	}
	if e = tw.Close(); e != nil {
		return "", e
	}
	return hashBytes(canonical), nil
}
func Verify(r io.Reader, trusted map[string]ed25519.PublicKey) (Verified, error) {
	var v Verified
	v.files = map[string][]byte{}
	tr := tar.NewReader(io.LimitReader(r, PackageLimit+8<<20))
	total := int64(0)
	seen := map[string]bool{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return v, e
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return v, errors.New("only ordinary files allowed in signed package")
		}
		if e = packagePath(h.Name); e != nil {
			return v, e
		}
		fold := strings.ToLower(h.Name)
		if seen[fold] {
			return v, errors.New("duplicate normalized package path")
		}
		seen[fold] = true
		if len(seen) > 10000 || h.Size < 0 || h.Size > PackageLimit-total {
			return v, errors.New("package size/member limit")
		}
		total += h.Size
		b, e := io.ReadAll(tr)
		if e != nil {
			return v, e
		}
		v.files[h.Name] = b
	}
	indexRaw := v.files["package-index.json"]
	if e := wire.Decode(indexRaw, &v.Index); e != nil {
		return v, e
	}
	if v.Index.FormatVersion != "1" {
		return v, errors.New("unsupported package format")
	}
	key, ok := trusted[v.Index.SigningKeyID]
	if !ok {
		return v, errors.New("unknown signing key; explicit trust review required")
	}
	canonical, e := operations.Canonical(v.Index)
	if e != nil {
		return v, e
	}
	if !bytes.Equal(indexRaw, canonical) {
		return v, errors.New("package inventory must be RFC 8785 canonical JSON")
	}
	if len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, canonical, v.files["package-index.sig"]) {
		return v, errors.New("invalid package signature")
	}
	v.Digest = hashBytes(canonical)
	previous := ""
	declared := map[string]File{}
	for _, f := range v.Index.Files {
		if e = packagePath(f.Path); e != nil {
			return v, e
		}
		if f.Path <= previous || f.Path == "package-index.json" || f.Path == "package-index.sig" {
			return v, errors.New("inventory paths must be unique, sorted payload paths")
		}
		previous = f.Path
		b, ok := v.files[f.Path]
		if !ok || int64(len(b)) != f.Size || hashBytes(b) != f.SHA256 {
			return v, fmt.Errorf("payload digest/size mismatch %s", f.Path)
		}
		declared[f.Path] = f
	}
	if len(v.files) != len(declared)+2 {
		return v, errors.New("unsigned extra package files")
	}
	v.Manifest, e = ValidateManifest(v.files["manifest.json"])
	if e != nil {
		return v, e
	}
	if v.Manifest.ID != v.Index.PluginID || v.Manifest.Version != v.Index.PluginVersion {
		return v, errors.New("manifest identity mismatch")
	}
	for _, entry := range v.Manifest.Entrypoints {
		f, ok := declared[entry.Path]
		if !ok || !f.Executable || entry.SHA256 != f.SHA256 {
			return v, errors.New("entrypoint lacks signed executable digest")
		}
	}
	return v, nil
}

// Extract writes only an already verified inventory into a new private directory.
func (v Verified) Extract(parent string) (string, error) {
	if len(v.files) == 0 {
		return "", errors.New("verified payload missing")
	}
	dir, e := os.MkdirTemp(parent, ".virmill-plugin-")
	if e != nil {
		return "", e
	}
	if e = os.Chmod(dir, 0700); e != nil {
		return "", e
	}
	r, e := os.OpenRoot(dir)
	if e != nil {
		return "", e
	}
	defer r.Close()
	for _, f := range v.Index.Files {
		if e = r.MkdirAll(filepath.Dir(f.Path), 0700); e != nil {
			return "", e
		}
		mode := os.FileMode(0600)
		if f.Executable {
			mode = 0700
		}
		dst, e := r.OpenFile(f.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			return "", e
		}
		_, e = dst.Write(v.files[f.Path])
		if e == nil {
			e = dst.Sync()
		}
		closeErr := dst.Close()
		if e != nil {
			return "", e
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return dir, nil
}

// Effective scopes are exact intersections. No scope string grants wildcard authority.
func Effective(declared, installed, invocation []Permission) []Permission {
	a, b := map[Permission]bool{}, map[Permission]bool{}
	for _, p := range installed {
		a[p] = true
	}
	for _, p := range invocation {
		b[p] = true
	}
	out := []Permission{}
	for _, p := range declared {
		if a[p] && b[p] {
			out = append(out, p)
		}
	}
	return out
}

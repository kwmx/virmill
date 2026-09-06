package plugins

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

type Developer struct {
	Engine       *operations.Engine
	SDKDirectory string
}

type developerInput struct {
	Action         string            `json:"action"`
	ID             string            `json:"id,omitempty"`
	Source         string            `json:"source"`
	Destination    string            `json:"destination"`
	SigningKeyPath string            `json:"signingKeyPath,omitempty"`
	SigningKeyID   string            `json:"signingKeyID,omitempty"`
	OutputDigest   string            `json:"outputDigest"`
	FileDigests    map[string]string `json:"fileDigests,omitempty"`
}

func (d *Developer) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in developerInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	return map[string]any{"action": in.Action, "pluginID": in.ID, "source": in.Source, "destination": in.Destination, "outputDigest": in.OutputDigest, "fileDigests": in.FileDigests, "signingKeyReference": in.SigningKeyPath, "signingKeyID": in.SigningKeyID, "overwriteExisting": false}, nil
}

func (d *Developer) prepare(in developerInput) ([]byte, map[string][]byte, error) {
	if in.Action == "new" {
		files, err := scaffoldFiles(in.Source, in.ID)
		return nil, files, err
	}
	// A signing key may not enter the payload even if the caller accidentally
	// selects its parent directory as the distribution root.
	root, err := filepath.EvalSymlinks(in.Source)
	if err != nil {
		return nil, nil, err
	}
	keyPath, err := filepath.EvalSymlinks(in.SigningKeyPath)
	if err != nil {
		return nil, nil, err
	}
	rel, err := filepath.Rel(root, keyPath)
	if err != nil {
		return nil, nil, err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, nil, domain.Fail("INVALID_INPUT", "signing key must be outside the distribution directory")
	}
	keyBytes, err := readRegular(in.SigningKeyPath, 256)
	if err != nil {
		return nil, nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(keyBytes)))
	clear(keyBytes)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, nil, domain.Fail("INVALID_INPUT", "private signing key must contain 64 hexadecimal-encoded Ed25519 key bytes")
	}
	defer clear(key)
	var out bytes.Buffer
	if _, err = Pack(root, ed25519.PrivateKey(key), in.SigningKeyID, &out); err != nil {
		return nil, nil, err
	}
	// Verify our own package before producing any distribution artifact.
	pub := ed25519.PrivateKey(key).Public().(ed25519.PublicKey)
	if _, err = Verify(bytes.NewReader(out.Bytes()), map[string]ed25519.PublicKey{in.SigningKeyID: pub}); err != nil {
		return nil, nil, err
	}
	return out.Bytes(), nil, nil
}

func fileDigests(files map[string][]byte) map[string]string {
	out := map[string]string{}
	for name, b := range files {
		out[name] = hashBytes(b)
	}
	return out
}

func absent(path string) error {
	_, err := os.Lstat(path)
	if !os.IsNotExist(err) {
		return domain.Fail("STALE_PLAN", "destination exists or cannot be proven absent; choose a new path")
	}
	return nil
}

func (d *Developer) Plan(ctx context.Context, uid uint32, action string, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	in := developerInput{Action: action}
	if action == "new" {
		if err := checkedInput(r.Input, "id", "sdkDirectory", "language", "type"); err != nil {
			return empty, err
		}
		in.ID, _ = r.Input["id"].(string)
		in.Source, _ = r.Input["sdkDirectory"].(string)
		if in.Source == "" {
			in.Source = d.SDKDirectory
		}
		for key, expected := range map[string]string{"language": "go", "type": "action"} {
			if value, ok := r.Input[key]; ok && value != expected {
				return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "scaffolder currently supports Go action source")
			}
		}
		in.Destination = r.Path
	} else if action == "pack" {
		if err := checkedInput(r.Input, "output", "signingKeyPath", "keyID"); err != nil {
			return empty, err
		}
		in.Source = r.Path
		in.Destination, _ = r.Input["output"].(string)
		in.SigningKeyPath, _ = r.Input["signingKeyPath"].(string)
		in.SigningKeyID, _ = r.Input["keyID"].(string)
	} else {
		return empty, domain.Fail("INVALID_INPUT", "unknown developer action")
	}
	if in.Source == "" || in.Destination == "" {
		return empty, domain.Fail("INVALID_INPUT", "source and destination are required")
	}
	var err error
	in.Source, err = filepath.Abs(in.Source)
	if err != nil {
		return empty, err
	}
	in.Destination, err = filepath.Abs(in.Destination)
	if err != nil {
		return empty, err
	}
	if in.SigningKeyPath != "" {
		in.SigningKeyPath, err = filepath.Abs(in.SigningKeyPath)
		if err != nil {
			return empty, err
		}
	}
	if err = absent(in.Destination); err != nil {
		return empty, err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(in.Destination))
	if err != nil {
		return empty, err
	}
	in.Destination = filepath.Join(parent, filepath.Base(in.Destination))
	payload, files, err := d.prepare(in)
	if err != nil {
		return empty, err
	}
	if files != nil {
		in.FileDigests = fileDigests(files)
		in.OutputDigest, err = operations.Digest(in.FileDigests)
	} else {
		in.OutputDigest = hashBytes(payload)
	}
	if err != nil {
		return empty, err
	}
	step := domain.Step{ID: "write", Action: "plugin." + action, Preconditions: []string{"source digest unchanged", "destination absent"}, Idempotency: "reconcile-before-retry", Compensation: "Retain any complete artifact; temporary preparation is private and not executable", Reconciliation: "Compare destination bytes against the planned digest without rerunning the plugin", CompletionPredicate: "Exact planned artifact exists at the approved destination"}
	return d.Engine.Plan(ctx, uid, "local", "plugin."+action, []string{"artifact:local:" + in.Destination}, nil, in, []domain.Step{step}, []string{"write-plugin-artifact"}, []string{"Creates new local developer files; no existing destination is overwritten", "No plugin code, installation hooks or build commands are executed"})
}

func (d *Developer) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var in developerInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	if err := absent(in.Destination); err != nil {
		return err
	}
	if p.Operation != "plugin."+in.Action {
		return domain.Fail("INVALID_INPUT", "developer plan action mismatch")
	}
	payload, files, err := d.prepare(in)
	if err != nil {
		return err
	}
	digest := hashBytes(payload)
	if files != nil {
		digest, err = operations.Digest(fileDigests(files))
		if err != nil {
			return err
		}
	}
	if digest != in.OutputDigest {
		return domain.Fail("SOURCE_CHANGED", "developer source or signing key changed after preview")
	}
	return nil
}

func (d *Developer) Execute(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) error {
	if err := d.Validate(ctx, p, b); err != nil {
		return err
	}
	var in developerInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	payload, files, err := d.prepare(in)
	if err != nil {
		return err
	}
	parent := filepath.Dir(in.Destination)
	if files != nil {
		digest, err := operations.Digest(fileDigests(files))
		if err != nil {
			return err
		}
		if digest != in.OutputDigest {
			return domain.Fail("SOURCE_CHANGED", "scaffold source changed before staging")
		}
		stage, err := os.MkdirTemp(parent, ".virmill-scaffold-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if err = writeScaffold(stage, files); err != nil {
			return err
		}
		if err = syncTree(stage); err != nil {
			return err
		}
		if err = renameNew(stage, in.Destination); err != nil {
			return err
		}
	} else {
		if hashBytes(payload) != in.OutputDigest {
			return domain.Fail("SOURCE_CHANGED", "package source changed before staging")
		}
		f, err := os.CreateTemp(parent, ".virmill-package-")
		if err != nil {
			return err
		}
		stage := f.Name()
		defer os.Remove(stage)
		_, err = f.Write(payload)
		if err == nil {
			err = f.Sync()
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err = renameNew(stage, in.Destination); err != nil {
			return err
		}
	}
	return syncDir(parent)
}

func (d *Developer) Reconcile(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var in developerInput
	if err := json.Unmarshal(b, &in); err != nil {
		return false, err
	}
	if in.Action == "pack" {
		actual, err := readRegular(in.Destination, PackageLimit+(8<<20))
		if os.IsNotExist(err) {
			return false, nil
		}
		return err == nil && hashBytes(actual) == in.OutputDigest, err
	}
	st, err := os.Lstat(in.Destination)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.IsDir() {
		return false, errors.New("scaffold destination is not an ordinary directory")
	}
	for name, digest := range in.FileDigests {
		actual, err := readRegular(filepath.Join(in.Destination, name), 2<<20)
		if err != nil {
			return false, err
		}
		if hashBytes(actual) != digest {
			return false, nil
		}
	}
	return true, nil
}

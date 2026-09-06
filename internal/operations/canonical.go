package operations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func Canonical(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	if e = wire.Validate(b); e != nil {
		return nil, e
	}
	return jsoncanonicalizer.Transform(b)
}
func Digest(v any) (string, error) {
	b, e := Canonical(v)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func PlanDigest(p domain.Plan) (string, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return "", e
	}
	var v map[string]any
	if e = json.Unmarshal(b, &v); e != nil {
		return "", e
	}
	delete(v, "planDigest")
	return Digest(v)
}

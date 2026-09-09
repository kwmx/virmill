// Package guestsetup executes reviewed, frozen guest recipes. Guest sudo is
// restricted to the exact built-in tools recipes and explicitly acknowledged.
package guestsetup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const recipeLimit = 256 << 10
const scriptLimit = 64 << 10

type Recipe struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"metadata"`
	Spec struct {
		Profile        string `json:"profile"`
		Privilege      string `json:"privilege"`
		Transport      string `json:"transport"`
		Idempotent     bool   `json:"idempotent"`
		TimeoutSeconds int    `json:"timeoutSeconds"`
		Check          string `json:"check"`
		Apply          string `json:"apply"`
		Verify         string `json:"verify"`
		Reboot         string `json:"reboot"`
	} `json:"spec"`
}

var canonicalUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var recipeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var recipeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func validUUID(value string) bool {
	return canonicalUUID.MatchString(value) && value != "00000000-0000-0000-0000-000000000000"
}
func hash(data []byte) string      { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func invalid(message string) error { return domain.Fail("INVALID_INPUT", "guest recipe: "+message) }
func safeText(value string, limit int, multiline bool) bool {
	if !utf8.ValidString(value) || len(value) > limit {
		return false
	}
	for _, r := range value {
		if unicode.Is(unicode.Cf, r) || unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\r' || r == '\t')) {
			return false
		}
	}
	return true
}

// strictDecode additionally rejects missing fields, null substitutes and case
// aliases by comparing the strict wire value with its complete typed shape.
func strictDecode(data []byte, value any) error {
	if err := wire.Decode(data, value); err != nil {
		return invalid("malformed or unknown JSON fields")
	}
	before, err := operations.Canonical(json.RawMessage(data))
	if err != nil {
		return invalid("invalid JSON shape")
	}
	after, err := operations.Canonical(value)
	if err != nil || !bytes.Equal(before, after) {
		return invalid("all exact JSON fields are required; null and case aliases are not substitutes")
	}
	return nil
}

func DecodeRecipe(data []byte) (Recipe, error) {
	var r Recipe
	if len(data) == 0 || len(data) > recipeLimit {
		return r, invalid("document must contain 1..262144 bytes")
	}
	if err := strictDecode(data, &r); err != nil {
		return Recipe{}, err
	}
	if err := r.Validate(); err != nil {
		return Recipe{}, err
	}
	return r, nil
}
func (r Recipe) Validate() error {
	if r.APIVersion != domain.APIVersion || r.Kind != "GuestRecipe" {
		return invalid("virmill/v1 GuestRecipe required")
	}
	if !recipeName.MatchString(r.Metadata.Name) || len(r.Metadata.Version) > 64 || !recipeVersion.MatchString(r.Metadata.Version) {
		return invalid("bounded lowercase recipe name and MAJOR.MINOR.PATCH version required")
	}
	if r.Spec.Profile != "linux-posix" || r.Spec.Transport != "ssh" || r.Spec.Reboot != "never" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "guest recipe requires linux-posix, SSH and no reboot")
	}
	if r.Spec.Privilege != "non-root" && (r.Spec.Privilege != "sudo" || !isBuiltinToolsRecipe(r)) {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "guest sudo is allowed only for exact built-in guest tools recipes")
	}
	if r.Spec.Privilege == "non-root" {
		declared := r
		declared.Spec.Privilege = "sudo"
		if isBuiltinToolsRecipe(declared) {
			return invalid("built-in guest tools require an explicit sudo privilege declaration")
		}
	}
	if r.Spec.TimeoutSeconds < 1 || r.Spec.TimeoutSeconds > 300 {
		return invalid("timeoutSeconds must be 1..300")
	}
	for _, script := range []string{r.Spec.Check, r.Spec.Apply, r.Spec.Verify} {
		if strings.TrimSpace(script) == "" || !safeText(script, scriptLimit, true) {
			return invalid("each script must contain bounded UTF-8 text without unsafe control characters")
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > recipeLimit {
		return invalid("encoded recipe exceeds the document limit")
	}
	return nil
}

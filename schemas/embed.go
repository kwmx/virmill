// Package schemas bundles logical schema IDs for strictly offline resolution.
package schemas

import "embed"

//go:embed *.schema.json
var Files embed.FS

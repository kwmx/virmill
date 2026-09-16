package buildinfo

import "runtime"

// Version is the product version source used by the binaries and package builder.
const Version = "1.0.0-beta.6"

var Revision = "uncommitted"
var BuildTime = "unknown"

func Info() map[string]any {
	return map[string]any{"product": "Virmill", "version": Version, "revision": Revision, "buildTime": BuildTime, "go": runtime.Version(), "os": runtime.GOOS, "architecture": runtime.GOARCH, "apiVersion": "virmill/v1", "pluginProtocol": "1.0", "releaseQualified": false}
}

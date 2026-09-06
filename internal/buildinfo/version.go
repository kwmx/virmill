package buildinfo

import "runtime"

var Version = "0.0.0-dev"
var Revision = "uncommitted"
var BuildTime = "unknown"

func Info() map[string]any {
	return map[string]any{"product": "Virmill", "version": Version, "revision": Revision, "buildTime": BuildTime, "go": runtime.Version(), "os": runtime.GOOS, "architecture": runtime.GOARCH, "apiVersion": "virmill/v1", "pluginProtocol": "1.0", "releaseQualified": false}
}

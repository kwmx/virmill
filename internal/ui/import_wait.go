package ui

import "time"

// Import checks hash complete source images before a plan can be reviewed.
// Keep a finite wait that accommodates large local media; cancellation remains
// available throughout. Explicit CLI --timeout values take precedence.
const ImportWait = 20 * time.Minute

func ImportRead(method string) bool {
	switch method {
	case "import.source.describe", "import.describe", "import.inspect", "import.prepare", "import.prepare-install", "import.prepare-disks":
		return true
	}
	return false
}

package ui

import "time"

// Import checks hash complete source images before a plan can be reviewed.
// Keep a finite wait that accommodates large local media; cancellation remains
// available throughout. Explicit CLI --timeout values take precedence.
const ImportWait = 20 * time.Minute

// LongWait reports methods that may read whole disk images before answering:
// import reads, and VM creation, which verifies every prepared image (ADR 0060).
// A 34 GiB prepared disk takes about 40 seconds to hash on the test host.
func LongWait(method string) bool {
	return ImportRead(method) || method == "vm.create"
}

func ImportRead(method string) bool {
	switch method {
	case "import.source.describe", "import.describe", "import.inspect", "import.prepare", "import.prepare-install", "import.prepare-disks":
		return true
	}
	return false
}

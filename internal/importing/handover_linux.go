//go:build linux && amd64

package importing

import (
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

// HandedOverKind records that a VM creation took over a preparation's disks
// (ADR 0060). From then on its prepared files may be partly released, and the
// preparation is never offered for another VM.
const HandedOverKind = "import-handed-over"

type HandOver struct {
	Version             int    `json:"version"`
	CreationPlanID      string `json:"creationPlanID"`
	CreationOperationID string `json:"creationOperationID"`
}

// RecordHandOver durably records the hand-over before any prepared file is
// released. A preparation is handed over at most once.
func RecordHandOver(db *store.Store, preparation string, h HandOver) error {
	if preparation == "" || h.Version != 1 || h.CreationPlanID == "" || h.CreationOperationID == "" {
		return domain.Fail("INVALID_INPUT", "incomplete prepared-copy hand-over record")
	}
	return db.ComparePut(HandedOverKind, preparation, nil, h)
}

func HandedOver(db *store.Store, preparation string) (HandOver, bool, error) {
	var h HandOver
	b, err := db.MetadataBytes(HandedOverKind, preparation)
	if err != nil || b == nil {
		return h, false, err
	}
	if err = wire.Decode(b, &h); err != nil {
		return h, true, err
	}
	if h.Version != 1 {
		return h, true, domain.Fail("UNSUPPORTED_CAPABILITY", "newer prepared-copy hand-over record refused")
	}
	return h, true, nil
}

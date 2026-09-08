//go:build !linux || !amd64

package helper

import (
	"context"
	"os"

	"virmill.local/core/internal/domain"
)

// AuxiliaryTransfer keeps shared type references buildable. Runtime auxiliary
// capture and authenticated descriptor transfer require the Linux amd64 helper.
type AuxiliaryTransfer struct {
	Response AuxiliaryResponse
	Snapshot *os.File
}

func (t *AuxiliaryTransfer) Commit(context.Context) (AuxiliaryResponse, error) {
	return AuxiliaryResponse{}, domain.Fail("UNSUPPORTED_CAPABILITY", "auxiliary capture delivery requires Linux amd64")
}

func (t *AuxiliaryTransfer) Close() error {
	if t.Snapshot != nil {
		return t.Snapshot.Close()
	}
	return nil
}

func ValidateAuxiliaryDelivery(Request, *AuxiliaryResponse) error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", "auxiliary capture delivery requires Linux amd64")
}

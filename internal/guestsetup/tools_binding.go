package guestsetup

import (
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

func toolsVMFingerprint(vm domain.VM) (string, error) {
	live, err := xmlpatch.GuestToolsLiveDigest(vm.LiveXML)
	if err != nil {
		return "", err
	}
	vm.LiveXML = live
	vm.Fingerprint = ""
	return operations.Digest(vm)
}

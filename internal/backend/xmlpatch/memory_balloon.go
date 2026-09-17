package xmlpatch

import "virmill.local/core/internal/domain"

// ReadMemoryBalloon reports the memory balloon model a definition carries, or
// "" when it has no balloon device. Only a balloon this reader understands
// counts: memory can be changed while a VM runs through a virtio balloon, and a
// device with settings Virmill does not model is not claimed to be one
// (ADR 0068).
func ReadMemoryBalloon(raw string) (string, error) {
	root, err := positionedXML(raw)
	if err != nil {
		return "", err
	}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "the definition has no single device list")
	}
	found := devices.children("memballoon")
	if len(found) == 0 {
		return "", nil
	}
	if len(found) > 1 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "the definition has more than one memory balloon")
	}
	model, ok := found[0].attr("model")
	if !ok {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "the memory balloon has no model")
	}
	return model, nil
}

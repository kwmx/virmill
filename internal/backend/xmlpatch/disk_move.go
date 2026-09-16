package xmlpatch

import "virmill.local/core/internal/domain"

// ADR 0062: point one existing disk at a copy of its volume in another pool.
// Only the disk's own source is rewritten: its target, bus, drive address,
// driver, boot order and every other device are left exactly as they were, so
// the guest sees the same disk in the same place with the same contents.
//
// Unlike adding a disk, this edit replaces a span in place rather than
// inserting an element, so the result keeps the document's child ordering and a
// whole-definition digest does bind it.
func RetargetDisk(data, target, pool, volume string) (string, error) {
	if !targetID.MatchString(target) {
		return "", domain.Fail("INVALID_INPUT", "stable disk target name required")
	}
	if !diskVolumeName.MatchString(pool) || !diskVolumeName.MatchString(volume) {
		return "", domain.Fail("INVALID_INPUT", "a moved disk needs an exact destination pool and volume")
	}
	_, disks, err := diskElements(data)
	if err != nil {
		return "", err
	}
	var node *positionedNode
	for _, disk := range disks {
		if disk.target == target {
			node = disk.node
			break
		}
	}
	if node == nil {
		return "", domain.Fail("INVALID_INPUT", "this VM has no disk "+target)
	}
	if device, _ := node.attr("device"); device != "disk" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "only a data disk can be moved; installer media and other devices cannot")
	}
	if len(node.children("readonly")) > 0 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "a read-only disk cannot be moved")
	}
	if len(node.children("auth")) > 0 || len(node.children("encryption")) > 0 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "this disk has authentication or encryption policy requiring a dedicated adapter")
	}
	for _, backing := range node.children("backingStore") {
		if len(backing.attrs) > 0 || len(backing.parts) > 0 {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "this disk has a nonempty backing graph; moving layered disks is not supported")
		}
	}
	if kind, _ := node.attr("type"); kind != "volume" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "only a disk already stored in a storage pool can be moved")
	}
	source, err := onlyChild(node, "source")
	if err != nil {
		return "", err
	}
	// The same attributes cold restore admits on a pool source. Anything else
	// carries policy this edit would silently drop.
	if !source.onlyAttrs("pool", "volume", "mode", "startupPolicy", "index") {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "this disk's source carries attributes requiring a dedicated adapter")
	}
	if !source.empty || len(source.parts) > 0 {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "a structured disk source cannot be moved by this editor")
	}
	oldPool, hasPool := source.attr("pool")
	oldVolume, hasVolume := source.attr("volume")
	if !hasPool || !hasVolume {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "this disk's source does not name a pool and volume")
	}
	if oldPool == pool && oldVolume == volume {
		return "", domain.Fail("STALE_PLAN", "this disk already uses that volume; review again")
	}
	replacement := `<source pool="` + restoreEscape(pool) + `" volume="` + restoreEscape(volume) + `"`
	for _, name := range []string{"mode", "startupPolicy", "index"} {
		if value, ok := source.attr(name); ok {
			replacement += ` ` + name + `="` + restoreEscape(value) + `"`
		}
	}
	replacement += `/>`
	return replaceSpans(data, []spanReplacement{{source.start, source.end, replacement}})
}

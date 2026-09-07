//go:build linux

// Package fileaccess implements bounded POSIX access-ACL transitions for managed
// ordinary files. It never reads or writes disk contents.
package fileaccess

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

const (
	aclVersion     = 2
	userObject     = 0x01
	namedUser      = 0x02
	groupObject    = 0x04
	namedGroup     = 0x08
	maskEntry      = 0x10
	otherEntry     = 0x20
	undefinedID    = ^uint32(0)
	maximumEntries = 128
)

type entry struct {
	tag         uint16
	permissions uint16
	id          uint32
}

// decode accepts only a canonical Linux POSIX ACL, including strictly increasing
// named IDs and exactly one base entry of each type. It does not repair input.
func decode(raw []byte) ([]entry, error) {
	if len(raw) < 4 || (len(raw)-4)%8 != 0 || len(raw) > 4+8*maximumEntries || binary.LittleEndian.Uint32(raw[:4]) != aclVersion {
		return nil, errors.New("unsupported POSIX access ACL encoding")
	}
	entries := []entry{}
	counts := map[uint16]int{}
	lastTag := uint16(0)
	lastID := uint32(0)
	for offset := 4; offset < len(raw); offset += 8 {
		e := entry{binary.LittleEndian.Uint16(raw[offset:]), binary.LittleEndian.Uint16(raw[offset+2:]), binary.LittleEndian.Uint32(raw[offset+4:])}
		if e.permissions&^uint16(7) != 0 {
			return nil, errors.New("invalid ACL permission bits")
		}
		switch e.tag {
		case userObject, groupObject, maskEntry, otherEntry:
			if e.id != undefinedID || counts[e.tag] != 0 {
				return nil, errors.New("duplicate or identified ACL base entry")
			}
		case namedUser, namedGroup:
			if e.id == undefinedID || (e.tag == lastTag && e.id <= lastID) {
				return nil, errors.New("invalid or duplicate named ACL ID")
			}
		default:
			return nil, errors.New("unknown ACL tag")
		}
		if e.tag < lastTag {
			return nil, errors.New("noncanonical ACL order")
		}
		lastTag, lastID = e.tag, e.id
		counts[e.tag]++
		entries = append(entries, e)
	}
	if counts[userObject] != 1 || counts[groupObject] != 1 || counts[otherEntry] != 1 || ((counts[namedUser] > 0 || counts[namedGroup] > 0) && counts[maskEntry] != 1) {
		return nil, errors.New("incomplete POSIX access ACL")
	}
	return entries, nil
}
func encode(entries []entry) []byte {
	raw := make([]byte, 4+8*len(entries))
	binary.LittleEndian.PutUint32(raw, aclVersion)
	for i, e := range entries {
		offset := 4 + i*8
		binary.LittleEndian.PutUint16(raw[offset:], e.tag)
		binary.LittleEndian.PutUint16(raw[offset+2:], e.permissions)
		binary.LittleEndian.PutUint32(raw[offset+4:], e.id)
	}
	return raw
}

// GrantRead derives an ACL with read permission for one non-root, non-owner UID.
// Other subjects retain their previous effective rights, including entries whose
// raw permissions were broader than the previous mask. It adds no data-write right.
func GrantRead(raw []byte, mode uint32, owner, group, actor uint32, actorGroups []uint32) ([]byte, error) {
	if actor == 0 || actor == undefinedID || actor == owner {
		return nil, errors.New("read grant needs a non-root actor different from the file owner")
	}
	if mode&07000 != 0 {
		return nil, errors.New("special permission bits need a separate access policy")
	}
	var entries []entry
	var err error
	if len(raw) == 0 {
		entries = []entry{{userObject, uint16(mode>>6) & 7, undefinedID}, {groupObject, uint16(mode>>3) & 7, undefinedID}, {otherEntry, uint16(mode) & 7, undefinedID}}
	} else {
		entries, err = decode(raw)
		if err != nil {
			return nil, err
		}
	}
	oldMask := uint16(mode>>3) & 7
	hasMask := false
	for _, e := range entries {
		switch e.tag {
		case userObject:
			if e.permissions != uint16(mode>>6)&7 {
				return nil, errors.New("ACL owner differs from file mode")
			}
		case otherEntry:
			if e.permissions != uint16(mode)&7 {
				return nil, errors.New("ACL other differs from file mode")
			}
		case maskEntry:
			if e.permissions != oldMask {
				return nil, errors.New("ACL mask differs from file mode")
			}
			hasMask = true
		}
	}
	if !hasMask {
		for _, e := range entries {
			if e.tag == groupObject && e.permissions != oldMask {
				return nil, errors.New("ACL group differs from file mode")
			}
		}
	}
	if len(actorGroups) > 4096 {
		return nil, errors.New("actor group inventory exceeds bound")
	}
	groups := map[uint32]bool{}
	for _, id := range actorGroups {
		if id == undefinedID {
			return nil, errors.New("invalid actor group")
		}
		groups[id] = true
	}
	prior := uint16(0)
	for _, permission := range []uint16{1, 2, 4} {
		if allowed(entries, owner, group, actor, groups, permission) {
			prior |= permission
		}
	}
	// A named entry cannot represent disjoint group write/execute combinations.
	// Refuse that change instead of granting a previously denied non-read request.
	if (prior&3 == 3) != allowed(entries, owner, group, actor, groups, 3) {
		return nil, errors.New("actor group rights cannot be preserved by one named read entry")
	}
	wanted := prior | 4
	newMask := oldMask | wanted
	found := false
	for i := range entries {
		e := &entries[i]
		if e.tag == namedUser || e.tag == groupObject || e.tag == namedGroup {
			e.permissions &= oldMask
		}
		if e.tag == namedUser && e.id == actor {
			e.permissions = wanted
			found = true
		}
		if e.tag == maskEntry {
			e.permissions = newMask
		}
	}
	if !found {
		entries = append(entries, entry{namedUser, wanted, actor})
	}
	if !hasMask {
		entries = append(entries, entry{maskEntry, newMask, undefinedID})
	}
	if len(entries) > maximumEntries {
		return nil, fmt.Errorf("ACL exceeds %d entries", maximumEntries)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].tag != entries[j].tag {
			return entries[i].tag < entries[j].tag
		}
		return entries[i].id < entries[j].id
	})
	out := encode(entries)
	if _, err = decode(out); err != nil {
		return nil, err
	}
	return out, nil
}

// allowed follows the POSIX ACL access-check order, including group matching
// before other and the requirement that one matching group grants the request.
func allowed(entries []entry, owner, group, actor uint32, groups map[uint32]bool, requested uint16) bool {
	if requested == 0 {
		return true
	}
	mask := uint16(7)
	other := uint16(0)
	for _, e := range entries {
		if e.tag == maskEntry {
			mask = e.permissions
		}
		if e.tag == otherEntry {
			other = e.permissions
		}
	}
	for _, e := range entries {
		if e.tag == userObject && actor == owner {
			return e.permissions&requested == requested
		}
		if e.tag == namedUser && e.id == actor {
			return (e.permissions&mask)&requested == requested
		}
	}
	matched := false
	for _, e := range entries {
		if (e.tag == groupObject && groups[group]) || (e.tag == namedGroup && groups[e.id]) {
			matched = true
			if (e.permissions&mask)&requested == requested {
				return true
			}
		}
	}
	return !matched && other&requested == requested
}

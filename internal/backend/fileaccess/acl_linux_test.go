//go:build linux

package fileaccess

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadGrantPreservesMaskedSubjectsAndPriorActorRights(t *testing.T) {
	// Every bit in these entries is broader than the original mask. Broadening a
	// mask without reducing those raw bits would silently grant unrelated rights.
	before := []entry{{userObject, 6, undefinedID}, {namedUser, 7, 2001}, {groupObject, 7, undefinedID}, {namedGroup, 7, 3001}, {maskEntry, 2, undefinedID}, {otherEntry, 1, undefinedID}}
	raw, err := GrantRead(encode(before), 0621, 0, 3000, 2000, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Target previously had execute through other. The read grant must preserve it.
	for requested := uint16(1); requested <= 7; requested++ {
		got := allowed(after, 0, 3000, 2000, nil, requested)
		want := (uint16(5) & requested) == requested
		if got != want {
			t.Fatalf("actor request %o got %v want %v", requested, got, want)
		}
	}
	for _, subject := range []struct {
		uid    uint32
		groups map[uint32]bool
	}{{0, nil}, {2001, nil}, {2100, map[uint32]bool{3000: true}}, {2101, map[uint32]bool{3001: true}}, {2102, nil}} {
		for req := uint16(1); req <= 7; req++ {
			if allowed(before, 0, 3000, subject.uid, subject.groups, req) != allowed(after, 0, 3000, subject.uid, subject.groups, req) {
				t.Fatalf("subject %d changed rights %o", subject.uid, req)
			}
		}
	}
	again, err := GrantRead(raw, 0671, 0, 3000, 2000, nil)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatalf("non-idempotent derivation: %v", err)
	}
}

func TestReadGrantExhaustiveSimpleModeAccess(t *testing.T) {
	for mode := uint32(0); mode < 01000; mode++ {
		before := []entry{{userObject, uint16(mode>>6) & 7, undefinedID}, {groupObject, uint16(mode>>3) & 7, undefinedID}, {otherEntry, uint16(mode) & 7, undefinedID}}
		for _, actorGroups := range [][]uint32{nil, {3000}} {
			groups := map[uint32]bool{}
			for _, g := range actorGroups {
				groups[g] = true
			}
			raw, err := GrantRead(nil, mode, 0, 3000, 2000, actorGroups)
			if err != nil {
				t.Fatalf("%o %v", mode, err)
			}
			after, err := decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			for req := uint16(1); req <= 7; req++ {
				// Add read to the target's existing rights; never introduce another right.
				if allowed(after, 0, 3000, 2000, groups, req) != allowed(before, 0, 3000, 2000, groups, req&^4) {
					t.Fatalf("mode %o request %o groups %v", mode, req, groups)
				}
				for _, uid := range []uint32{0, 2001} {
					if allowed(before, 0, 3000, uid, groups, req) != allowed(after, 0, 3000, uid, groups, req) {
						t.Fatal("unrelated subject changed")
					}
				}
			}
		}
	}
}

func TestDisjointGroupRightsRefusedWithoutWriteEscalation(t *testing.T) {
	raw := encode([]entry{{userObject, 6, undefinedID}, {groupObject, 2, undefinedID}, {namedGroup, 1, 3001}, {maskEntry, 3, undefinedID}, {otherEntry, 0, undefinedID}})
	if _, err := GrantRead(raw, 0630, 0, 3000, 2000, []uint32{3000, 3001}); err == nil {
		t.Fatal("disjoint write/execute combined by read grant")
	}
	if _, err := GrantRead(raw, 0630, 0, 3000, 2000, []uint32{3000}); err != nil {
		t.Fatal(err)
	}
}

func TestACLInputRefusals(t *testing.T) {
	valid := encode([]entry{{userObject, 6, undefinedID}, {groupObject, 0, undefinedID}, {otherEntry, 0, undefinedID}})
	cases := [][]byte{{}, {2, 0, 0, 0}, append(append([]byte{}, valid...), 0), append(append([]byte{}, valid...), valid[4:12]...)}
	for _, modify := range []func([]byte){func(b []byte) { b[0] = 9 }, func(b []byte) { b[4] = 99 }, func(b []byte) { b[6] = 8 }, func(b []byte) { binary.LittleEndian.PutUint32(b[8:], 0) }, func(b []byte) { b[12] = maskEntry }} {
		b := append([]byte{}, valid...)
		modify(b)
		cases = append(cases, b)
	}
	for _, raw := range cases {
		if _, err := decode(raw); err == nil {
			t.Fatalf("invalid ACL accepted: %x", raw)
		}
	}
	for _, c := range []struct{ mode, owner, actor uint32 }{{0600, 0, 0}, {0600, 2000, 2000}, {04600, 0, 2000}, {0640, 0, 2000}} {
		if _, err := GrantRead(valid, c.mode, c.owner, 3000, c.actor, nil); err == nil {
			t.Fatalf("invalid grant accepted: %+v", c)
		}
	}
}

func FuzzACLReadGrant(f *testing.F) {
	f.Add(encode([]entry{{userObject, 6, undefinedID}, {groupObject, 0, undefinedID}, {otherEntry, 0, undefinedID}}), uint32(0600))
	f.Fuzz(func(t *testing.T, raw []byte, mode uint32) {
		result, err := GrantRead(raw, mode, 0, 3000, 2000, nil)
		if err == nil {
			if _, e := decode(result); e != nil {
				t.Fatal(e)
			}
		}
	})
}

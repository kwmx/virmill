package devices

import "testing"

func TestUSBReconnectionAmbiguityAndHostUse(t *testing.T) {
	s := Selector{Vendor: "1234", Product: "5678", Serial: "same", Missing: "required"}
	a := USB{ID: "a", Vendor: s.Vendor, Product: s.Product, Serial: s.Serial, Port: "1-1", HostUseKnown: true, Bus: 1, Device: 2}
	b := a
	b.ID = "b"
	b.Port = "1-2"
	if _, _, e := Resolve(s, []USB{a, b}, "vm"); e == nil {
		t.Fatal("ambiguous serial chosen")
	}
	s.Port = "1-1"
	a.Device = 9
	r, _, e := Resolve(s, []USB{a, b}, "vm")
	if e != nil || r.Device != 9 {
		t.Fatal("stale bus address", e)
	}
	a.Mounted = true
	if _, _, e = Resolve(s, []USB{a}, "vm"); e == nil {
		t.Fatal("mounted storage accepted")
	}
	s.Missing = "optional"
	if d, w, e := Resolve(s, nil, "vm"); d != nil || len(w) != 1 || e != nil {
		t.Fatal("optional missing policy")
	}
}

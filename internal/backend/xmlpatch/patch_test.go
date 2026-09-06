package xmlpatch

import (
	"strings"
	"testing"
)

func TestPreservesOpaqueXML(t *testing.T) {
	x := `<domain xmlns:qemu="http://libvirt.org/schemas/domain/qemu/1.0"><name>vm</name><vcpu placement="static">2</vcpu><metadata><other:thing xmlns:other="urn:vendor" foo="bar">opaque</other:thing></metadata><!--keep--><devices><disk vendor="unrecognized"/></devices><qemu:commandline><qemu:arg value="expert"/></qemu:commandline></domain>`
	got, e := Patch(x, map[string]string{"domain/vcpu": "4"})
	if e != nil {
		t.Fatal(e)
	}
	if got != strings.Replace(x, ">2</vcpu>", ">4</vcpu>", 1) {
		t.Fatal("unrelated XML changed")
	}
}
func TestRefusesAmbiguousEdits(t *testing.T) {
	for _, x := range []string{`<domain><vcpu>2</vcpu><vcpu>3</vcpu></domain>`, `<!DOCTYPE domain><domain><vcpu>2</vcpu></domain>`, `<domain><vcpu><x/></vcpu></domain>`} {
		if _, e := Patch(x, map[string]string{"domain/vcpu": "4"}); e == nil {
			t.Fatal("accepted lossy XML")
		}
	}
}
func FuzzPatch(f *testing.F) {
	f.Add(`<domain><vcpu>2</vcpu></domain>`)
	f.Fuzz(func(t *testing.T, s string) { _, _ = Patch(s, map[string]string{"domain/vcpu": "4"}) })
}

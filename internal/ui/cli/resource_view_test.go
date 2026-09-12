package cli

import (
	"bytes"
	"testing"
)

func TestResourceViewUsesReadOnlySharedMethod(t *testing.T) {
	r := &recorder{}
	var out bytes.Buffer
	c := New(r, &out, &out)
	c.SetArgs([]string{"vm", "resources", "show", "570c4866-5b5e-4583-817c-602538d19b9c", "--connection", "qemu:///session", "--output", "json", "--non-interactive"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if r.method != "vm.resources.show" || r.request.Connection != "qemu:///session" || r.request.ID != "570c4866-5b5e-4583-817c-602538d19b9c" || r.request.Apply != nil || len(r.request.Input) != 0 {
		t.Fatal(r)
	}
}

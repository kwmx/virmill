package cli

import (
	"bytes"
	"testing"
)

func TestRebootUsesSharedPlanWithoutApplying(t *testing.T) {
	r := &recorder{}
	var out bytes.Buffer
	c := New(r, &out, &out)
	c.SetArgs([]string{"vm", "reboot", "selected-uuid", "--connection", "qemu:///session", "--plan", "--output", "json", "--non-interactive"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if r.method != "vm.plan" || r.request.Action != "reboot" || r.request.ID != "selected-uuid" || r.request.Connection != "qemu:///session" || r.request.Apply != nil {
		t.Fatal("reboot bypassed plan", r)
	}
}

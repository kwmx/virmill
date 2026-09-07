package cli

import (
	"bytes"
	"testing"
)

func TestReadOnlyHostInventoryCommands(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		method string
	}{
		{[]string{"host", "pci", "list"}, "host.pci.list"},
		{[]string{"network", "cidr", "check", "--input", `{"candidates":["10.77.0.0/24"],"planned":[{"id":"lab","cidr":"10.78.0.0/24"}]}`}, "network.cidr.check"},
	} {
		r := &recorder{}
		var out bytes.Buffer
		cmd := New(r, &out, &out)
		cmd.SetArgs(append(tc.args, "--connection", "qemu:///session", "--output", "json"))
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if r.method != tc.method || r.request.Connection != "qemu:///session" || r.request.ID != "" || r.request.Action != "" || r.request.Apply != nil {
			t.Fatal("read-only inventory request changed", r)
		}
		if tc.method == "network.cidr.check" && len(r.request.Input) != 2 {
			t.Fatal("CIDR input lost", r)
		}
	}
	r := &recorder{}
	var out bytes.Buffer
	cmd := New(r, &out, &out)
	cmd.SetArgs([]string{"network", "cidr", "check", "unexpected-position"})
	if err := cmd.Execute(); err == nil || r.method != "" {
		t.Fatal("unexpected positional argument reached service")
	}
}

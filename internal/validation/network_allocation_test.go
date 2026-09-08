package validation

import (
	"os"
	"testing"
)

func TestNetworkAllocationSettingsSchema(t *testing.T) {
	good, err := os.ReadFile("../../examples/network-allocation.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = Schema("network-allocation-settings", good); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{}`, `{"version":2,"ranges":[{"cidr":"10.0.0.0/8","prefixLength":24}]}`,
		`{"version":1,"ranges":[]}`, `{"version":1,"ranges":[{"cidr":"10.0.0.0/8","prefixLength":31}]}`,
		`{"version":1,"ranges":[{"cidr":"10.0.0.0/8","prefixLength":24,"unknown":true}]}`,
		`{"version":1,"ranges":[{"cidr":"10.0.0.0/8","prefixLength":24}],"planned":[{"id":"bad id","cidr":"10.1.0.0/24"}]}`,
	} {
		if Schema("network-allocation-settings", []byte(raw)) == nil {
			t.Fatal("invalid settings shape accepted", raw)
		}
	}
}

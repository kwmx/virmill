package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func TestJobSummariesAddPlanOperationAndTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	vm := "libvirt|qemu:///system|vm|12345678-1234-4234-8234-123456789abc"
	start := domain.Plan{ID: domain.ID(), ActorUID: 1000, ConnectionID: "fixture", Operation: "vm.start", ResourceIDs: []string{vm},
		Review: map[string]any{"vmName": "Build guest", "xml": strings.Repeat("<large/>", 1000)}}
	network := domain.Plan{ID: domain.ID(), ActorUID: 1000, ConnectionID: "fixture", Operation: "network.create", ResourceIDs: []string{"fixture-network"}}
	var jobs []domain.Job
	for _, p := range []domain.Plan{start, network} {
		if err = s.SavePlan(p, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		j, err := s.Accept(p, p.ID, "digest")
		if err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, j)
	}
	list, err := s.JobSummaries()
	if err != nil || len(list) != 2 {
		t.Fatal(err, list)
	}
	if list[0].ID != jobs[1].ID || list[0].Operation != "network.create" || list[0].TargetName != "" ||
		!reflect.DeepEqual(list[0].ResourceIDs, []string{"fixture-network"}) {
		t.Fatalf("newest job summary %+v", list[0])
	}
	one := list[1]
	if one.ID != jobs[0].ID || one.Operation != "vm.start" || one.TargetName != "Build guest" ||
		one.State != "queued" || one.PlanID != start.ID || !reflect.DeepEqual(one.ResourceIDs, []string{vm}) {
		t.Fatalf("job summary %+v", one)
	}
	body, _ := json.Marshal(one)
	if !strings.Contains(string(body), `"operationID":"`+jobs[0].ID+`"`) || !strings.Contains(string(body), `"operation":"vm.start"`) {
		t.Fatalf("summary JSON %s", body)
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

type recorder struct {
	method  string
	request app.Request
}

func (r *recorder) Call(ctx context.Context, method string, p app.Request) (app.Response, error) {
	r.method = method
	r.request = p
	return app.Response{APIVersion: domain.APIVersion, Data: map[string]string{"name": "fixture\x1b[2J"}, Warnings: []string{}}, nil
}
func TestCleanJSONNoninteractiveAndSharedMethod(t *testing.T) {
	r := &recorder{}
	var out, errs bytes.Buffer
	c := New(r, &out, &errs)
	c.SetArgs([]string{"vm", "show", "uuid-fixture", "--output", "json", "--non-interactive"})
	if e := c.Execute(); e != nil {
		t.Fatal(e)
	}
	if r.method != "inventory.get" || r.request.ID != "uuid-fixture" {
		t.Fatal("wrong service path")
	}
	var response app.Response
	if e := json.Unmarshal(out.Bytes(), &response); e != nil {
		t.Fatal("dirty stdout", e)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("raw terminal escape")
	}
	c = New(r, &out, &errs)
	c.SetArgs([]string{"vm", "set", "id", "--input", `{"vcpus":2,"vcpus":4}`})
	if e := c.Execute(); e == nil {
		t.Fatal("duplicate parameter accepted")
	}
}
func TestPlanApplyRequiresExplicitBinding(t *testing.T) {
	r := &recorder{}
	var out bytes.Buffer
	c := New(r, &out, &out)
	c.SetArgs([]string{"plan", "apply", "id", "--non-interactive"})
	if e := c.Execute(); e == nil || r.method != "" {
		t.Fatal("unbound mutation dispatched")
	}
}

func TestPluginPlansUseSharedServiceAndExplicitInputs(t *testing.T) {
	r := &recorder{}
	var out bytes.Buffer
	c := New(r, &out, &out)
	c.SetArgs([]string{"plugin", "new", "/tmp/source", "--id", "example.virmill.source", "--language", "go", "--type", "action", "--plan", "--output", "json"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if r.method != "plugin.develop" || r.request.Action != "new" || r.request.Input["id"] != "example.virmill.source" {
		t.Fatal("scaffold bypassed service", r)
	}
	c = New(r, &out, &out)
	c.SetArgs([]string{"plugin", "call", "example.virmill.source", "summary", "--input", `{"vmIDs":["fixture"],"parameters":{}}`, "--output", "json"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if r.method != "plugin.call" || r.request.ID != "example.virmill.source" || r.request.Input["action"] != "summary" {
		t.Fatal("invocation bypassed service", r)
	}
}

func TestImportPreparationUsesSharedService(t *testing.T) {
	r := &recorder{}
	var out bytes.Buffer
	c := New(r, &out, &out)
	c.SetArgs([]string{"import", "prepare", "/tmp/source.ova", "--input", `{"destination":"/tmp/private/prepared","disks":[{"id":"boot","format":"vmdk","maximumVirtualBytes":16777216}]}`, "--plan", "--output", "json", "--non-interactive"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if r.method != "import.prepare" || r.request.Path != "/tmp/source.ova" || r.request.Input["destination"] != "/tmp/private/prepared" {
		t.Fatal("import preview bypassed shared service", r)
	}
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const networkRecordKind = "network-creation-v1"

type networkRecipe struct {
	Version     int                      `json:"version"`
	Definition  domain.NetworkDefinition `json:"definition"`
	Metadata    json.RawMessage          `json:"metadata"`
	ParentJobID string                   `json:"parentJobID,omitempty"`
	OriginJobID string                   `json:"originJobID,omitempty"`
	Allocation  *networkAllocationReview `json:"allocation,omitempty"`
}
type networkRecord struct {
	Version    int                      `json:"version"`
	Connection string                   `json:"connection"`
	JobID      string                   `json:"jobID"`
	PlanID     string                   `json:"planID"`
	PlanDigest string                   `json:"planDigest"`
	Definition domain.NetworkDefinition `json:"definition"`
	Metadata   json.RawMessage          `json:"metadata"`
}

func networkResources(uri string, d domain.NetworkDefinition) []string {
	out := []string{(domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "network", UUID: d.UUID}).String(), (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "network-allocation", UUID: "host"}).String()}
	slices.Sort(out)
	return out
}
func parseNetworkRecipe(p domain.Plan, raw []byte) (networkRecipe, error) {
	var r networkRecipe
	if wire.Decode(raw, &r) != nil || r.Version < 1 || r.Version != r.Definition.PolicyVersion() || p.ConnectionID != "qemu:///system" || !slices.Equal(p.ResourceIDs, networkResources(p.ConnectionID, r.Definition)) || len(p.Before) != 0 || networkxml.Validate(r.Definition) != nil {
		return r, domain.Fail("INVALID_INPUT", "invalid managed network creation binding")
	}
	if (p.Operation != "network.create" || r.ParentJobID != "" || r.OriginJobID != "") && (p.Operation != "network.creation.resume" || r.ParentJobID == "" || r.OriginJobID == "") {
		return r, domain.Fail("INVALID_INPUT", "invalid network recovery binding")
	}
	if err := validateNetworkAllocation(r); err != nil {
		return r, err
	}
	var metadata map[string]any
	if json.Unmarshal(r.Metadata, &metadata) != nil {
		return r, domain.Fail("INVALID_INPUT", "invalid network metadata")
	}
	return r, nil
}
func networkSteps(resume bool, d domain.NetworkDefinition) []domain.Step {
	step := func(id, action, predicate string) domain.Step {
		return domain.Step{ID: id, Action: action, Preconditions: []string{"exact new network identity", "no conflicting host or reserved prefix", "exclusive network writer"}, Idempotency: "non-repeatable", Compensation: "Retain this network and its subnet reservation for reviewed recovery; never remove unrelated resources", Reconciliation: "Observe the exact journal-bound network without replaying definition or activation", CompletionPredicate: predicate}
	}
	out := []domain.Step{}
	if !resume {
		out = append(out, step("define", "network.define", "Exact persistent network exists and is inactive with autostart disabled"))
	}
	if d.PolicyVersion() == 2 {
		out = append(out, step("firewall", "network.policy-filter", "Exact host-access and IPv6 rules are present in runtime and permanent configuration with verified host-input chain ordering"))
		return append(out, step("activate", "network.activate", "Exact persistent network is active with autostart disabled and protected policy rules present; packet and guest behavior remain unverified"))
	}
	out = append(out, step("firewall", "network.ipv6-filter", "Exact bridge IPv6 deny rules are present in firewalld runtime and permanent configuration"))
	return append(out, step("activate", "network.activate", "Exact persistent network is active with autostart disabled and IPv6 deny rules present; packet and guest behavior remain unverified"))
}
func networkAcknowledgements() []string {
	return []string{"host-mutation", "network-host-access", "network-firewall", "exclusive-network-writer"}
}
func networkRisks(d domain.NetworkDefinition) []string {
	if d.PolicyVersion() == 2 {
		profile := "The bridge has no host IPv4 address, DHCP, DNS or forwarding; declared IPv4 CIDR reserves only the logical guest subnet, and guests need explicit static addressing"
		if d.HostAccess == "services-only" {
			profile = "Host access permits only managed DHCP and DNS when DHCP is enabled; disabling DHCP also disables managed DNS. Other host IPv4 services are denied"
		}
		return []string{profile, "Host-access and IPv6 filters require a separate protectedNetworks administrator grant and exact supported firewall chain ordering; drift causes refusal", "Libvirt creates only the reviewed new virtual bridge and native forwarding; no physical uplink or global host setting is changed", "Coordinate other network writers: libvirt has no atomic create-only definition API and host/route snapshots are not atomic", "Packet, IPv6 and guest routing qualification is separate; autostart remains disabled"}
	}
	return []string{"The new segment explicitly permits host access; it is unsuitable for untrusted guests requiring host isolation", "Libvirt creates only the reviewed new bridge, IPv4 address, optional DHCP/DNS and native NAT or isolated forwarding; no physical uplink is moved", "Coordinate other network writers: libvirt has no atomic create-only definition API and host/route snapshots are not atomic", "IPv6, DHCP, routing, DNS and packet isolation require native verification; autostart remains disabled"}
}
func (s *Service) planNetworkCreation(ctx context.Context, uid uint32, r Request) (domain.Plan, error) {
	var empty domain.Plan
	if r.Path == "" || r.ID != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 || r.Action != "create" || r.Connection != "qemu:///system" {
		return empty, domain.Fail("INVALID_INPUT", "network creation requires one Network document and qemu:///system")
	}
	b, err := readDeclaration(ctx, r.Path)
	if err != nil {
		return empty, err
	}
	value, _, err := validation.Document(b)
	if err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	if value["kind"] != "Network" {
		return empty, domain.Fail("INVALID_INPUT", "expected a Network declaration")
	}
	specRaw, _ := json.Marshal(value["spec"])
	var spec network.Spec
	// Extensions must not be silently discarded by a core-only creation workflow.
	if err = wire.Decode(specRaw, &spec); err != nil {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "network creation cannot apply extension fields or unknown intent")
	}
	if err = network.Validate(spec); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	if (spec.IPv4 == nil && spec.Type != "guest-only") || spec.BridgeRef != "" {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "creation requires a new managed bridge and IPv4 CIDR or auto, optional only for guest-only")
	}
	id := domain.ID()
	d := domain.NetworkDefinition{UUID: id, Name: "virmill-" + id, Bridge: "vm" + strings.ReplaceAll(id, "-", "")[:12], Type: spec.Type, IPv6Mode: spec.IPv6.Mode, HostAccess: spec.HostAccess, Egress: spec.Egress}
	if spec.IPv4 != nil {
		d.IPv4CIDR, d.DHCPEnabled, d.AdvertiseDefaultRoute = spec.IPv4.CIDR, spec.IPv4.DHCP.Enabled, spec.IPv4.DHCP.AdvertiseDefaultRoute
	}
	var allocation *networkAllocationReview
	if d.IPv4CIDR == "auto" {
		var selected string
		selected, allocation, err = s.allocateNetworkCIDR(ctx, r.Connection)
		if err != nil {
			return empty, err
		}
		d.IPv4CIDR = selected
	}
	if err = networkxml.Validate(d); err != nil {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", err.Error())
	}
	metadata, _ := json.Marshal(value["metadata"])
	recipe := networkRecipe{Version: d.PolicyVersion(), Definition: d, Metadata: metadata, Allocation: allocation}
	return s.Engine.Plan(ctx, uid, r.Connection, "network.create", networkResources(r.Connection, d), nil, recipe, networkSteps(false, d), networkAcknowledgements(), networkRisks(d))
}
func (s *Service) networkRecords() ([]networkRecord, error) {
	entries, err := s.Engine.Store.MetadataRecords()
	if err != nil {
		return nil, err
	}
	records := []networkRecord{}
	ids := map[string]bool{}
	for _, entry := range entries {
		if entry.Kind != networkRecordKind {
			continue
		}
		if len(records) >= 4096 {
			return nil, domain.Fail("INVALID_STATE", "network reservation limit")
		}
		var record networkRecord
		if wire.Decode(entry.Body, &record) != nil || record.Version < 1 || record.Version != record.Definition.PolicyVersion() || record.Connection != "qemu:///system" || record.JobID == "" || record.PlanID == "" || len(record.PlanDigest) != 64 || networkxml.Validate(record.Definition) != nil || ids[record.Definition.UUID] || entry.ID != record.Definition.UUID {
			return nil, domain.Fail("INVALID_STATE", "network reservation is corrupt; preserve allocations")
		}
		plan, raw, err := s.Engine.Store.Plan(record.PlanID)
		if err != nil {
			return nil, err
		}
		recipe, err := parseNetworkRecipe(plan, raw)
		if err != nil {
			return nil, err
		}
		verified, err := s.recordForRecipe(plan, recipe)
		if err != nil || verified.JobID == "" || verified.Definition != record.Definition {
			return nil, domain.Fail("SOURCE_CHANGED", "network reservation integrity differs; preserve allocations")
		}
		ids[record.Definition.UUID] = true
		records = append(records, record)
	}
	return records, nil
}
func (s *Service) checkNetworkCIDR(ctx context.Context, uri string, d domain.NetworkDefinition, exclude string) error {
	if d.IPv4CIDR == "" && d.Type == "guest-only" {
		// No subnet is allocated. Still validate retained reservations so
		// corrupt state cannot be hidden by an addressless profile.
		if _, err := s.networkRecords(); err != nil {
			return err
		}
		return ctx.Err()
	}
	result, err := s.checkCIDRsExcept(ctx, Request{Connection: uri, Input: map[string]any{"candidates": []string{d.IPv4CIDR}}}, exclude)
	if err != nil {
		return err
	}
	report, ok := result.(cidrReport)
	if !ok || len(report.Candidates) != 1 {
		return domain.Fail("INVALID_STATE", "incomplete subnet observation")
	}
	if len(report.Candidates[0].Conflicts) > 0 {
		return domain.Fail("CIDR_CONFLICT", fmt.Sprintf("subnet %s conflicts with %d observed or reserved allocations; run network cidr check", d.IPv4CIDR, len(report.Candidates[0].Conflicts)))
	}
	return ctx.Err()
}
func (s *Service) recordForRecipe(p domain.Plan, r networkRecipe) (networkRecord, error) {
	var out networkRecord
	raw, err := s.Engine.Store.MetadataBytes(networkRecordKind, r.Definition.UUID)
	if err != nil {
		return out, err
	}
	if len(raw) == 0 {
		return out, nil
	}
	if wire.Decode(raw, &out) != nil || out.Version != r.Version || out.Connection != p.ConnectionID || out.Definition != r.Definition || !reflect.DeepEqual(json.RawMessage(out.Metadata), r.Metadata) {
		return out, domain.Fail("SOURCE_CHANGED", "network reservation differs from immutable recipe")
	}
	original, body, err := s.Engine.Store.Plan(out.PlanID)
	if err != nil {
		return out, err
	}
	digest, err := operations.PlanDigest(original)
	if err != nil || digest != out.PlanDigest || digest != original.Digest {
		return out, domain.Fail("SOURCE_CHANGED", "original network plan integrity differs")
	}
	recipe, err := parseNetworkRecipe(original, body)
	if err != nil || recipe.Definition != r.Definition || !reflect.DeepEqual(recipe.Allocation, r.Allocation) || original.Operation != "network.create" || original.ActorUID != p.ActorUID {
		return out, domain.Fail("SOURCE_CHANGED", "network reservation does not belong to original creation")
	}
	inputDigest, err := operations.Digest(json.RawMessage(body))
	if err != nil || inputDigest != original.InputDigest {
		return out, domain.Fail("SOURCE_CHANGED", "original network input digest differs")
	}
	job, err := s.Engine.Store.Job(out.JobID)
	if err != nil || job.ID != out.JobID || job.PlanID != out.PlanID {
		return out, domain.Fail("SOURCE_CHANGED", "network reservation job binding differs")
	}
	return out, nil
}

type networkCreationHandler struct {
	s *Service
}

func (*networkCreationHandler) RetainCompletedEffects() bool { return true }

func (h *networkCreationHandler) Review(ctx context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	r, err := parseNetworkRecipe(p, raw)
	if err != nil {
		return nil, err
	}
	xml, err := networkxml.Render(r.Definition)
	if err != nil {
		return nil, err
	}
	return map[string]any{"allocation": r.Allocation, "definition": r.Definition, "metadata": r.Metadata, "networkXML": xml, "autostart": false, "packetVerification": "not-run", "guestRoutingVerified": false, "resumeFrom": r.ParentJobID, "physicalUplinkChanges": false}, ctx.Err()
}
func (h *networkCreationHandler) Estimate(context.Context, domain.Plan, []byte) (domain.Estimates, error) {
	return domain.Estimates{Notes: "One new virtual bridge and optional libvirt DHCP/DNS process; no guest or disk allocation. Existing host networking is retained."}, nil
}
func (h *networkCreationHandler) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	r, err := parseNetworkRecipe(p, raw)
	if err != nil {
		return err
	}
	provider, ok := h.s.Provider.(domain.NetworkCreationProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "native network creation unavailable")
	}
	if h.s.NetworkFirewall == nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "authenticated network policy helper unavailable")
	}
	// Preview is possible before an administrator approves its fresh UUID. The
	// sealed plan supplies that concrete identity; apply must pass helper policy.
	if p.Digest != "" {
		if err = h.s.NetworkFirewall.Check(ctx, p, r.Definition); err != nil {
			return err
		}
	}
	record, err := h.s.recordForRecipe(p, r)
	if err != nil {
		return err
	}
	if r.ParentJobID != "" {
		if record.JobID == "" || record.JobID != r.OriginJobID {
			return domain.Fail("SOURCE_CHANGED", "recovery has no exact original subnet reservation")
		}
		if err = h.checkParent(ctx, p, r); err != nil {
			return err
		}
	} else if record.JobID != "" && (record.JobID != operations.OperationID(ctx) || record.PlanID != p.ID || record.PlanDigest != p.Digest) {
		return domain.Fail("RESOURCE_BUSY", "network identity is already reserved")
	}
	if record.JobID == "" {
		if err = provider.CheckNetworkCreation(ctx, p.ConnectionID, r.Definition); err != nil {
			return err
		}
		return h.s.checkNetworkCIDR(ctx, p.ConnectionID, r.Definition, "")
	}
	n, err := provider.InspectCreatedNetwork(ctx, p.ConnectionID, r.Definition)
	if err != nil {
		return err
	}
	if n.Active || !n.Persistent || n.Autostart {
		return domain.Fail("STALE_PLAN", "activation requires the exact inactive persistent network with autostart disabled")
	}
	return h.s.checkNetworkCIDR(ctx, p.ConnectionID, r.Definition, r.Definition.UUID)
}
func (h *networkCreationHandler) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	r, err := parseNetworkRecipe(p, raw)
	if err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" {
		return domain.Fail("INVALID_INPUT", "network change requires durable job identity")
	}
	provider, ok := h.s.Provider.(domain.NetworkCreationProvider)
	if !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "native network creation unavailable")
	}
	switch step.ID {
	case "define":
		if r.ParentJobID != "" {
			return domain.Fail("INVALID_INPUT", "recovery cannot redefine a network")
		}
		if err = provider.CheckNetworkCreation(ctx, p.ConnectionID, r.Definition); err != nil {
			return err
		}
		if err = h.s.checkNetworkCIDR(ctx, p.ConnectionID, r.Definition, ""); err != nil {
			return err
		}
		record := networkRecord{Version: r.Version, Connection: p.ConnectionID, JobID: id, PlanID: p.ID, PlanDigest: p.Digest, Definition: r.Definition, Metadata: r.Metadata}
		if err = h.s.Engine.Store.ComparePut(networkRecordKind, r.Definition.UUID, nil, record); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		return provider.DefineNetwork(ctx, p.ConnectionID, r.Definition)
	case "firewall":
		if err = h.Validate(ctx, p, raw); err != nil {
			return err
		}
		return h.s.NetworkFirewall.Apply(ctx, p, r.Definition, id)
	case "activate":
		if err = h.Validate(ctx, p, raw); err != nil {
			return err
		}
		if err = h.s.NetworkFirewall.Observe(ctx, p, r.Definition, id); err != nil {
			return err
		}
		return provider.ActivateNetwork(ctx, p.ConnectionID, r.Definition)
	default:
		return domain.Fail("INVALID_INPUT", "unknown network creation step")
	}
}
func (h *networkCreationHandler) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	r, err := parseNetworkRecipe(p, raw)
	if err != nil {
		return false, err
	}
	record, err := h.s.recordForRecipe(p, r)
	if err != nil {
		return false, err
	}
	origin := operations.OperationID(ctx)
	if r.OriginJobID != "" {
		origin = r.OriginJobID
	}
	if record.JobID == "" || record.JobID != origin {
		return false, domain.Fail("RECOVERY_REQUIRED", "exact network reservation is absent; no replay is permitted")
	}
	provider, ok := h.s.Provider.(domain.NetworkCreationProvider)
	if !ok {
		return false, domain.Fail("UNSUPPORTED_CAPABILITY", "native network inspection unavailable")
	}
	n, err := provider.InspectCreatedNetwork(ctx, p.ConnectionID, r.Definition)
	if err != nil {
		return false, err
	}
	if !n.Persistent || n.Autostart {
		return false, domain.Fail("RECOVERY_REQUIRED", "network persistence or autostart differs")
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	switch step.ID {
	case "define":
		return !n.Active, nil
	case "firewall":
		if n.Active {
			return false, domain.Fail("RECOVERY_REQUIRED", "network activated before firewall step completed")
		}
		if h.s.NetworkFirewall == nil {
			return false, domain.Fail("UNSUPPORTED_CAPABILITY", "network filter observation unavailable")
		}
		err = h.s.NetworkFirewall.Observe(ctx, p, r.Definition, operations.OperationID(ctx))
		return err == nil, err
	case "activate":
		if h.s.NetworkFirewall == nil {
			return false, domain.Fail("UNSUPPORTED_CAPABILITY", "network filter observation unavailable")
		}
		err = h.s.NetworkFirewall.Observe(ctx, p, r.Definition, operations.OperationID(ctx))
		return n.Active && err == nil, err
	default:
		return false, domain.Fail("INVALID_INPUT", "unknown network reconciliation step")
	}
}
func (h *networkCreationHandler) checkParent(ctx context.Context, p domain.Plan, r networkRecipe) error {
	j, err := h.s.Engine.Store.Job(r.ParentJobID)
	if err != nil {
		return err
	}
	prior, _, err := h.s.Engine.Store.Plan(j.PlanID)
	if err != nil {
		return err
	}
	if prior.ActorUID != p.ActorUID || prior.ConnectionID != p.ConnectionID || !slices.Equal(prior.ResourceIDs, p.ResourceIDs) {
		return domain.Fail("SOURCE_CHANGED", "recovery parent identity differs")
	}
	active := operations.OperationID(ctx)
	if active != "" && j.State == "partial" && j.RecoveryOperationID == active {
		return ctx.Err()
	}
	if j.RecoveryOperationID != "" || (j.State != "recovery-required" && j.State != "interrupted") {
		return domain.Fail("STALE_PLAN", "recovery parent is not unresolved")
	}
	return ctx.Err()
}

// Only this wrapper implements lock inheritance; ordinary creation never does.
type networkResumeHandler struct{ networkCreationHandler }

func (h *networkResumeHandler) RecoveryParent(ctx context.Context, p domain.Plan, raw []byte) (string, error) {
	r, err := parseNetworkRecipe(p, raw)
	if err != nil {
		return "", err
	}
	return r.ParentJobID, h.checkParent(ctx, p, r)
}
func (s *Service) planNetworkResume(ctx context.Context, uid uint32, r Request) (domain.Plan, error) {
	var empty domain.Plan
	if r.ID == "" || r.Path != "" || len(r.Input) != 0 || r.Apply != nil || r.After != 0 || r.Action != "resume" || r.Connection != "qemu:///system" {
		return empty, domain.Fail("INVALID_INPUT", "network recovery requires an unresolved creation operation ID")
	}
	j, err := s.Engine.Store.Job(r.ID)
	if err != nil {
		return empty, err
	}
	p, raw, err := s.Engine.Store.Plan(j.PlanID)
	if err != nil {
		return empty, err
	}
	old, err := parseNetworkRecipe(p, raw)
	if err != nil {
		return empty, err
	}
	digest, err := operations.PlanDigest(p)
	if err != nil || digest != p.Digest {
		return empty, domain.Fail("SOURCE_CHANGED", "recovery parent plan digest differs")
	}
	inputDigest, err := operations.Digest(json.RawMessage(raw))
	if err != nil || inputDigest != p.InputDigest {
		return empty, domain.Fail("SOURCE_CHANGED", "recovery parent input digest differs")
	}
	if old.OriginJobID == "" {
		old.OriginJobID = j.ID
	}
	old.ParentJobID = j.ID
	return s.Engine.Plan(ctx, uid, r.Connection, "network.creation.resume", networkResources(r.Connection, old.Definition), nil, old, networkSteps(true, old.Definition), networkAcknowledgements(), networkRisks(old.Definition))
}
func (s *Service) networkCreationResult(ctx context.Context, uid uint32, r Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.ID == "" || r.Path != "" || r.Action != "" || r.Apply != nil || r.After != 0 || len(r.Input) != 0 {
		return nil, domain.Fail("INVALID_INPUT", "creation result requires only an operation ID")
	}
	j, err := s.Engine.Store.Job(r.ID)
	if err != nil {
		return nil, err
	}
	p, raw, err := s.Engine.Store.Plan(j.PlanID)
	if err != nil {
		return nil, err
	}
	recipe, err := parseNetworkRecipe(p, raw)
	if err != nil {
		return nil, err
	}
	if p.ActorUID != uid || p.ConnectionID != r.Connection {
		return nil, domain.Fail("PERMISSION_DENIED", "creation belongs to another actor or connection")
	}
	record, err := s.recordForRecipe(p, recipe)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"job": j, "allocation": recipe.Allocation, "definition": recipe.Definition, "metadata": recipe.Metadata, "subnetReserved": record.JobID != "" && recipe.Definition.IPv4CIDR != "", "packetVerification": "not-run", "guestRoutingVerified": false}
	if provider, ok := s.Provider.(domain.NetworkCreationProvider); ok && record.JobID != "" {
		n, e := provider.InspectCreatedNetwork(ctx, r.Connection, recipe.Definition)
		if e != nil {
			result["observationError"] = validation.SafeText(e.Error())
		} else {
			result["network"] = n
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

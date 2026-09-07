//go:build linux && cgo

package libvirt

import (
	"context"
	"virmill.local/core/internal/domain"
)

var _ domain.ColdStateInspector = (*Provider)(nil)

// InspectColdState does not open auxiliary state or grant read authority. The
// full persistent XML is still authoritative, including opaque configuration.
func (p *Provider) InspectColdState(ctx context.Context, uri, id string) (domain.ColdStateInspection, error) {
	if err := ctx.Err(); err != nil {
		return domain.ColdStateInspection{}, err
	}
	vm, err := p.Get(ctx, uri, id)
	if err != nil {
		return domain.ColdStateInspection{}, err
	}
	return coldStateInspection(ctx, vm)
}

func coldStateInspection(ctx context.Context, vm domain.VM) (domain.ColdStateInspection, error) {
	var out domain.ColdStateInspection
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if vm.PersistentXML == "" {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "cold recovery inspection requires a persistent domain definition")
	}
	layout, err := InspectColdStateXML(vm.PersistentXML)
	if err != nil {
		return out, err
	}
	if layout.VMID != vm.Key.UUID {
		return out, domain.Fail("INVALID_STATE", "persistent XML identity differs from native VM identity")
	}
	out = domain.ColdStateInspection{Resource: vm.Key, State: vm.State, HasManagedSave: vm.HasManagedSave, Autostart: vm.Autostart, Fingerprint: vm.Fingerprint, Layout: layout, Warnings: []string{"Configuration observation only. No disk, NVRAM, TPM or secret bytes have been captured or verified."}}
	if vm.State != "stopped" {
		out.Warnings = append(out.Warnings, "Complete auxiliary-state capture requires an approved shutdown and a confirmed stopped VM; do not copy live NVRAM or TPM files.")
	}
	if vm.HasManagedSave {
		out.Warnings = append(out.Warnings, "Managed-save state requires explicit resolution before cold capture; it is not a matching independent backup.")
	}
	if vm.Autostart {
		out.Warnings = append(out.Warnings, "Autostart is enabled. Any capture plan must account for restart and writer exclusion; this inspection changes no policy.")
	}
	if layout.TPM != nil && layout.TPM.SourcePath == "" {
		out.Warnings = append(out.Warnings, "Native XML does not identify the TPM state path. A verified host adapter must resolve it before any capture; no path was guessed.")
	}
	if len(layout.SecretReferences) > 0 {
		out.Warnings = append(out.Warnings, "External secret references are dependencies, not recoverable secret values. Independent recovery requires an explicit supported inclusion or dependency policy.")
	}
	if err := ctx.Err(); err != nil {
		return domain.ColdStateInspection{}, err
	}
	return out, nil
}

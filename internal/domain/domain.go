// Package domain contains platform-neutral product contracts.
package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const APIVersion = "virmill/v1"

type Error struct {
	Code            string   `json:"code"`
	Message         string   `json:"message"`
	Resource        string   `json:"resource"`
	OperationID     string   `json:"operationID"`
	Retryable       bool     `json:"retryable"`
	SafeNextActions []string `json:"safeNextActions"`
	Details         any      `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func Fail(code, msg string) *Error {
	return &Error{Code: code, Message: msg, SafeNextActions: []string{}}
}
func ExitCode(e error) int {
	if e == nil {
		return 0
	}
	if d, ok := e.(*Error); ok {
		switch d.Code {
		case "INVALID_INPUT":
			return 2
		case "UNSUPPORTED_CAPABILITY", "NOT_IMPLEMENTED":
			return 3
		case "PERMISSION_REQUIRED", "PERMISSION_DENIED":
			return 4
		case "STALE_PLAN", "RESOURCE_BUSY", "IDEMPOTENCY_CONFLICT":
			return 5
		case "PARTIAL_APPLY", "RECOVERY_REQUIRED":
			return 6
		case "WAIT_TIMEOUT":
			return 7
		case "CLIENT_INTERRUPTED":
			return 130
		}
	}
	return 1
}
func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	s := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[:8], s[8:12], s[12:16], s[16:20], s[20:])
}

type ResourceKey struct {
	ProviderID   string `json:"providerID"`
	ConnectionID string `json:"connectionID"`
	Kind         string `json:"kind"`
	UUID         string `json:"resourceUUID"`
}

func (k ResourceKey) String() string {
	return strings.Join([]string{k.ProviderID, k.ConnectionID, k.Kind, k.UUID}, "|")
}

type Capability struct {
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	ReasonCode    string   `json:"reasonCode"`
	Reason        string   `json:"reason"`
	Alternatives  []string `json:"alternatives"`
	EvidenceClass string   `json:"evidenceClass"`
	// Optional guidance; absent from older coordinators.
	Purpose   string   `json:"purpose,omitempty"`
	Optional  bool     `json:"optional,omitempty"`
	Packages  []string `json:"packages,omitempty"`
	Installer string   `json:"installer,omitempty"`
}
type VM struct {
	Key            ResourceKey `json:"key"`
	Name           string      `json:"name"`
	State          string      `json:"state"`
	Ownership      string      `json:"ownership"`
	PersistentXML  string      `json:"persistentXML"`
	LiveXML        string      `json:"liveXML,omitempty"`
	Fingerprint    string      `json:"fingerprint"`
	Autostart      bool        `json:"autostart"`
	Tags           []string    `json:"tags"`
	HasManagedSave bool        `json:"hasManagedSave"`
}

// ConfigurationValidator repeats provider-specific preservation checks before
// journal acceptance and execution. It never mutates the domain.
type ConfigurationValidator interface {
	CheckConfiguration(context.Context, string, string, map[string]any) error
	ObserveConfiguration(context.Context, string, string, map[string]any) (bool, error)
}
type Grant struct {
	Operation  string `json:"operation"`
	ResourceID string `json:"resourceID"`
}
type Step struct {
	ID                  string   `json:"id"`
	Action              string   `json:"action"`
	Preconditions       []string `json:"preconditions"`
	Idempotency         string   `json:"idempotency"`
	Compensation        string   `json:"compensation"`
	Reconciliation      string   `json:"reconciliation"`
	CompletionPredicate string   `json:"completionPredicate"`
}
type Estimates struct {
	AdditionalBytes  uint64 `json:"additionalBytes"`
	RequiresDowntime bool   `json:"requiresDowntime"`
	Notes            string `json:"notes,omitempty"`
}
type Plan struct {
	APIVersion       string            `json:"apiVersion"`
	ID               string            `json:"planID"`
	CreatedAt        time.Time         `json:"createdAt"`
	ExpiresAt        time.Time         `json:"expiresAt"`
	ActorUID         uint32            `json:"actorUID"`
	ConnectionID     string            `json:"connectionID"`
	Operation        string            `json:"operation"`
	ResourceIDs      []string          `json:"resourceIDs"`
	InputDigest      string            `json:"inputDigest"`
	Digest           string            `json:"planDigest"`
	Before           map[string]string `json:"beforeFingerprints"`
	RequiredGrants   []Grant           `json:"requiredGrants"`
	Acknowledgements []string          `json:"acknowledgements"`
	Risks            []string          `json:"risks"`
	Steps            []Step            `json:"steps"`
	Estimates        Estimates         `json:"estimates"`
	Review           map[string]any    `json:"review,omitempty"`
}
type Event struct {
	APIVersion  string    `json:"apiVersion"`
	OperationID string    `json:"operationID"`
	Seq         int64     `json:"sequence"`
	At          time.Time `json:"timestamp"`
	Phase       string    `json:"phase"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
}
type Job struct {
	ID                  string    `json:"operationID"`
	PlanID              string    `json:"planID"`
	State               string    `json:"state"`
	Step                int       `json:"step"`
	CreatedAt           time.Time `json:"createdAt"`
	Error               *Error    `json:"error"`
	CancelRequested     bool      `json:"cancelRequested"`
	RecoveryOf          string    `json:"recoveryOf,omitempty"`
	RecoveryOperationID string    `json:"recoveryOperationID,omitempty"`
}

func Terminal(s string) bool {
	switch s {
	case "succeeded", "failed", "partial", "canceled", "recovery-required":
		return true
	}
	return false
}

type ComputeProvider interface {
	List(context.Context, string) ([]VM, error)
	Get(context.Context, string, string) (VM, error)
	Capabilities(context.Context, string) ([]Capability, error)
	Execute(context.Context, string, string, string, map[string]any) error
}
type NetworkProvider interface {
	ObserveNetwork(context.Context, ResourceKey) (any, error)
}
type StorageProvider interface {
	ObserveVolume(context.Context, ResourceKey) (any, error)
}
type DeviceProvider interface {
	Inventory(context.Context) (any, error)
}
type GuestTransport interface {
	CheckIdentity(context.Context, ResourceKey) error
}
type ServiceManager interface {
	Status(context.Context) (any, error)
}
type CredentialStore interface {
	Use(context.Context, string, func([]byte) error) error
}
type SandboxRunner interface {
	Run(context.Context, []string) ([]byte, error)
}
type ConsoleLauncher interface {
	Launch(context.Context, ResourceKey) error
}

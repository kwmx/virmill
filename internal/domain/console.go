package domain

import "context"

// ConsoleInfo describes observed local guest access without exposing endpoints
// or changing guest configuration. Availability does not prove a guest login.
type ConsoleInfo struct {
	Resource          ResourceKey     `json:"resource"`
	Name              string          `json:"name"`
	State             string          `json:"state"`
	ConfigFingerprint string          `json:"configFingerprint"`
	Choices           []ConsoleChoice `json:"choices"`
	Warnings          []string        `json:"warnings"`
}

type ConsoleChoice struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Protocol      string `json:"protocol"`
	Label         string `json:"label"`
	Device        string `json:"device,omitempty"`
	GraphicsIndex int    `json:"graphicsIndex,omitempty"`
	Available     bool   `json:"available"`
	Reason        string `json:"reason,omitempty"`
}

type ConsoleInspector interface {
	InspectConsole(context.Context, string, string) (ConsoleInfo, error)
}

package domain

import "time"

// BackupReceipt contains recovery selectors, never credentials or authority.
// Restoring still verifies the selected encrypted repository and every member.
type BackupReceipt struct {
	APIVersion     string    `json:"apiVersion"`
	Kind           string    `json:"kind"`
	OperationID    string    `json:"operationID"`
	Connection     string    `json:"connection"`
	Repository     string    `json:"repository"`
	CaptureID      string    `json:"captureID"`
	SnapshotID     string    `json:"snapshotID"`
	ManifestSHA256 string    `json:"manifestSHA256"`
	VerifiedAt     time.Time `json:"verifiedAt"`
}

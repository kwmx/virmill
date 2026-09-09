package importer

// SourceDescription is unverified, read-only input metadata. Preparation must
// independently verify and bind every selected file before producing artifacts.
type SourceDescription struct {
	Source        string       `json:"source"`
	Kind          string       `json:"kind"`
	Root          string       `json:"root,omitempty"`
	Name          string       `json:"name"`
	Format        string       `json:"format,omitempty"`
	PhysicalBytes int64        `json:"physicalBytes"`
	Disks         []SourceDisk `json:"disks"`
	Files         []string     `json:"files"`
	Warnings      []string     `json:"warnings"`
	Appliance     *Report      `json:"appliance,omitempty"`
}
type SourceDisk struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	Format        string `json:"format"`
	VirtualBytes  int64  `json:"virtualBytes"`
	PhysicalBytes int64  `json:"physicalBytes"`
	BackingPath   string `json:"backingPath,omitempty"`
	BackingFormat string `json:"backingFormat,omitempty"`
}

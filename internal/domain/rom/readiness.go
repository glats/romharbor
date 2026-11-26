package rom

const (
	ReadinessComplete   = "complete"
	ReadinessIncomplete = "incomplete"
	ReadinessUnknown    = "unknown"
)

type GameReadiness struct {
	Status        string                     `json:"status"`
	EntryPoint    string                     `json:"entry_point,omitempty"`
	Files         []string                   `json:"files,omitempty"`
	MissingFiles  []string                   `json:"missing_files,omitempty"`
	MissingDiscs  []int                      `json:"missing_discs,omitempty"`
	Reason        string                     `json:"reason,omitempty"`
	BundleReady   bool                       `json:"bundle_ready"`
	Compatibility []DestinationCompatibility `json:"compatibility,omitempty"`
}

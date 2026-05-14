package ops

type Result struct {
	Summary string         `json:"summary"`
	Output  string         `json:"output,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

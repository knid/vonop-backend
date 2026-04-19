package responses

// ValidationResult is returned by the connection validation endpoints.
// Valid=true means the credentials successfully authenticated against
// the external API. When Apps is populated, the UI can render
// "We saw 3 apps in your account, including TaskTracker Pro" for trust.
type ValidationResult struct {
	Valid   bool     `json:"valid"`
	Message string   `json:"message"`
	Apps    []string `json:"apps,omitempty"` // app display names the key can see
}

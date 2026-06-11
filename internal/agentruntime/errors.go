package agentruntime

import "fmt"

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("agentruntime: runtime returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("agentruntime: runtime returned status %d: %s", e.StatusCode, e.Body)
}

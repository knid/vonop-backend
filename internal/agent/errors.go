package agent

import "fmt"

// ErrorCode classifies why a run stopped.
type ErrorCode string

const (
	CodeCreditsExhausted ErrorCode = "CREDITS_EXHAUSTED"
	CodeRateLimited      ErrorCode = "RATE_LIMITED"
	CodeProviderError    ErrorCode = "PROVIDER_ERROR"
	// CodeBudgetExceeded = a declared budget was hit (wallclock deadline or
	// token cap). Distinct from CodeCanceled, which is an external cancel.
	CodeBudgetExceeded ErrorCode = "BUDGET_EXCEEDED"
	// CodeCanceled = the run was canceled externally (user hit Stop, process
	// shutdown). Separate from CodeBudgetExceeded so the UI can tell the user
	// "you (or the server) canceled" vs "the run ran too long".
	CodeCanceled      ErrorCode = "CANCELED"
	CodeToolFailed    ErrorCode = "TOOL_FAILED"
	CodeGitError      ErrorCode = "GIT_ERROR"
	CodeRunInProgress ErrorCode = "RUN_IN_PROGRESS"
	CodeInternal      ErrorCode = "INTERNAL"
)

// Error is a typed agent error that carries a phase label and an error code.
// Returned from Run() so callers can map to HTTP status codes and SSE event
// payloads without string-sniffing.
type Error struct {
	Code    ErrorCode
	Phase   string // "setup" | "agent_loop" | "tool" | "commit"
	Message string
	Wrapped error
}

func (e *Error) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("%s: %s: %s: %v", e.Phase, e.Code, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("%s: %s: %s", e.Phase, e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Wrapped }

// New builds a typed agent error.
func New(code ErrorCode, phase, msg string, wrapped error) *Error {
	return &Error{Code: code, Phase: phase, Message: msg, Wrapped: wrapped}
}

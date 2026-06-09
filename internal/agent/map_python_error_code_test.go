package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapPythonErrorCode_RuntimeError(t *testing.T) {
	assert.Equal(t, CodeRuntimeError, mapPythonErrorCode("runtime_error"))
	assert.Equal(t, CodeRuntimeError, mapPythonErrorCode("runtime_fault"))
	assert.Equal(t, CodeRuntimeError, mapPythonErrorCode("python_crash"))
}

func TestMapPythonErrorCode_ExistingMappingsPreserved(t *testing.T) {
	assert.Equal(t, CodeCanceled, mapPythonErrorCode("cancelled"))
	assert.Equal(t, CodeCanceled, mapPythonErrorCode("cancellation"))
	assert.Equal(t, CodeRateLimited, mapPythonErrorCode("rate_limited"))
	assert.Equal(t, CodeTruncated, mapPythonErrorCode("truncated"))
	assert.Equal(t, CodeRepetitionLoop, mapPythonErrorCode("repetition_loop"))
	assert.Equal(t, CodeToolRoundsExceeded, mapPythonErrorCode("tool_rounds_exceeded"))
	assert.Equal(t, CodeValidationFailed, mapPythonErrorCode("validation_failed"))
	assert.Equal(t, CodeBudgetExceeded, mapPythonErrorCode("timeout"))
	assert.Equal(t, CodeBudgetExceeded, mapPythonErrorCode("deadline_exceeded"))
	assert.Equal(t, CodeProviderError, mapPythonErrorCode("unknown_error"))
}

func TestMapPythonErrorCode_CaseInsensitive(t *testing.T) {
	assert.Equal(t, CodeRuntimeError, mapPythonErrorCode("RUNTIME_ERROR"))
	assert.Equal(t, CodeRuntimeError, mapPythonErrorCode("  runtime_error  "))
	assert.Equal(t, CodeCanceled, mapPythonErrorCode("CANCELLED"))
}

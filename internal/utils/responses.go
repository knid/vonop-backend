package utils

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/knid/wemob/internal/constants"
)

func JSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Slice && v.IsNil() {
		data = reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

// JSONError is the canonical way to send any error response. The response
// always includes a stable machine-readable code; the English message is
// a fallback for unknown codes / non-localized clients. Pass details as
// nil if there are none.
func JSONError(w http.ResponseWriter, code, message string, status int, details map[string]any) {
	resp := map[string]any{
		"error": message,
		"code":  code,
	}
	if details != nil {
		resp["details"] = details
	}
	JSONResponse(w, resp, status)
}

// JSONErrorCode is sugar when the default English message from the
// constants catalog is fine. Looks up constants.DefaultMessages[code]
// and falls back to a generic string if the code is unknown.
func JSONErrorCode(w http.ResponseWriter, code string, status int, details map[string]any) {
	msg, ok := constants.DefaultMessages[code]
	if !ok {
		msg = "An error occurred."
	}
	JSONError(w, code, msg, status, details)
}

// JSONInternalError logs the underlying error and returns INTERNAL_ERROR
// with no leaked detail to the client. Use anywhere an unexpected error
// would otherwise surface as `err.Error()` in a 5xx response.
func JSONInternalError(w http.ResponseWriter, err error) {
	slog.Error("internal error", "error", err)
	JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
}

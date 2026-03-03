package utils

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
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

func JSONErrorResponse(w http.ResponseWriter, message string, code int) {
	JSONResponse(w, map[string]string{"error": message}, code)
}

// JSONCodedErrorResponse returns a structured error with a machine-readable code,
// human-readable message, and optional details. This enables frontends to show
// contextual upgrade prompts on specific error codes.
func JSONCodedErrorResponse(w http.ResponseWriter, errorCode string, message string, statusCode int, details map[string]any) {
	resp := map[string]any{
		"error":   message,
		"code":    errorCode,
	}
	if details != nil {
		resp["details"] = details
	}
	JSONResponse(w, resp, statusCode)
}

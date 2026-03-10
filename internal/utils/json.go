package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxRequestBodySize is the maximum allowed request body size (10 MB).
const maxRequestBodySize = 10 * 1024 * 1024

func ParseJSONBody(r *http.Request, dst interface{}) error {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("unsupported content type: %s", ct)
	}
	reader := io.LimitReader(r.Body, maxRequestBodySize)
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	return nil
}

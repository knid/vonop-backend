package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxRequestBodySize is the maximum allowed request body size (25 MB).
// Needs to be large enough for base64-encoded image attachments in chat/generation requests.
const maxRequestBodySize = 25 * 1024 * 1024

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

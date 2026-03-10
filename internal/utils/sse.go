package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func SetSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func WriteSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventName string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, string(jsonData)); err != nil {
		return err
	}

	flusher.Flush()
	return nil
}

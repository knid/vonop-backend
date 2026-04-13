package utils

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

func SetSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

// WriteSSEEvent emits one SSE frame.
//
// `seq` is the per-job monotonic sequence number; when non-zero it's emitted
// as the SSE `id: <seq>` line. Browsers treat `id:` as the "Last-Event-ID"
// resume cursor, and our custom parser does the same. Pass 0 for frames that
// have no useful sequence (bootstrap `job_started`, the `stream_state`
// reconnect snapshot, terminal markers).
func WriteSSEEvent(w http.ResponseWriter, flusher http.Flusher, seq int64, eventName string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		slog.Error("WriteSSEEvent: failed to marshal data", "event", eventName, "error", err)
		return err
	}

	if seq > 0 {
		if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, eventName, string(jsonData)); err != nil {
			return err
		}
	} else {
		if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, string(jsonData)); err != nil {
			return err
		}
	}

	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

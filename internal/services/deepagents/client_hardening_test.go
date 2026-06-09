package deepagents

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFakeClientWithStream(t *testing.T, body string) *Client {
	t.Helper()
	return &Client{
		BaseURL: "http://example.local",
		Token:   "token",
		HTTPClient: &http.Client{
			Transport: roundTripFn(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: 200,
					Status:     "OK",
					Header:     http.Header{"Content-Type": []string{"application/x-ndjson"}},
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    req,
				}, nil
			}),
		},
	}
}

func newFakeClientWithStatus(t *testing.T, status int, body string) *Client {
	t.Helper()
	return &Client{
		BaseURL: "http://example.local",
		Token:   "token",
		HTTPClient: &http.Client{
			Transport: roundTripFn(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Status:     http.StatusText(status),
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    req,
				}, nil
			}),
		},
	}
}

func TestClientHeartbeat_ReturnsErrorForNilClient(t *testing.T) {
	var c *Client
	err := c.Heartbeat(t.Context(), "run-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil client")
}

func TestClientHeartbeat_RequiresRunID(t *testing.T) {
	c := NewClient("http://localhost:1234", "token")
	err := c.Heartbeat(t.Context(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run_id is required")
}

func TestClientMalformedEvent_SkippedNotAborted(t *testing.T) {
	receivedEvents := make([]DeepAgentsEvent, 0)
	client := newFakeClientWithStream(t, `{"type":"message_delta","data":{"delta":"ok","turn":1}}
this line is not valid json
{"type":"done","data":{"summary":"done","turns":1,"tool_calls":0}}
`)
	err := client.StreamRun(t.Context(), DeepAgentsRunRequest{
		RunID: "run-1",
	}, func(evt DeepAgentsEvent) error {
		receivedEvents = append(receivedEvents, evt)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, receivedEvents, 2)
	assert.Equal(t, "message_delta", receivedEvents[0].Type)
	assert.Equal(t, "done", receivedEvents[1].Type)
}

func TestClientStream_HandlesConnectionDrop(t *testing.T) {
	streamBody := `{"type":"message_delta","data":{"delta":"ok"}}
{"type":"done","data":{"summary":"done","turns":1}}
{"type":"message_delta","data":{"delta":"partial`
	client := newFakeClientWithStream(t, streamBody)
	receivedEvents := make([]DeepAgentsEvent, 0)
	err := client.StreamRun(t.Context(), DeepAgentsRunRequest{
		RunID: "run-1",
	}, func(evt DeepAgentsEvent) error {
		receivedEvents = append(receivedEvents, evt)
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, receivedEvents, 2, "expected 2 valid events before stream error")
}

func TestClientStream_ReportsNon2xxStatus(t *testing.T) {
	client := newFakeClientWithStatus(t, 502, "gateway timeout")
	err := client.StreamRun(t.Context(), DeepAgentsRunRequest{
		RunID: "run-1",
	}, func(evt DeepAgentsEvent) error {
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deepagents:")
}

func TestClientCancel_HandlesNotFound(t *testing.T) {
	client := newFakeClientWithStatus(t, 404, `{"status":"not_found"}`)
	err := client.CancelRun(t.Context(), "run-gone")
	require.NoError(t, err)
}

func TestClientCancel_ReturnsErrorForNon2xx(t *testing.T) {
	client := newFakeClientWithStatus(t, 500, "internal error")
	err := client.CancelRun(t.Context(), "run-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancel failed")
}

func TestClientHeartbeat_ReturnsErrorForNon200(t *testing.T) {
	client := newFakeClientWithStatus(t, 500, "internal error")
	err := client.Heartbeat(t.Context(), "run-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "heartbeat failed")
}

func TestClientHeartbeat_SucceedsFor200(t *testing.T) {
	client := newFakeClientWithStatus(t, 200, `{"status":"active"}`)
	err := client.Heartbeat(t.Context(), "run-1")
	require.NoError(t, err)
}

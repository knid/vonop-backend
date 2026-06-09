package deepagents

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type roundTripFn func(*http.Request) (*http.Response, error)

func (fn roundTripFn) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newFakeClient(t *testing.T, handler func(*http.Request) *http.Response) *Client {
	t.Helper()

	return &Client{
		BaseURL: "http://example.local",
		Token:   "token",
		HTTPClient: &http.Client{
			Transport: roundTripFn(func(req *http.Request) (*http.Response, error) {
				return handler(req), nil
			}),
		},
	}
}

func responseFromText(status int, body string, headers ...map[string]string) *http.Response {
	header := make(http.Header)
	for _, h := range headers {
		for k, v := range h {
			header.Set(k, v)
		}
	}
	return &http.Response{
		StatusCode: status,
		Status:     "OK",
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    &http.Request{},
	}
}

func TestStreamRunParsesScaffoldEvents(t *testing.T) {
	received := make([]DeepAgentsEvent, 0, 2)
	client := newFakeClient(t, func(req *http.Request) *http.Response {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "/internal/v1/runs/stream", req.URL.Path)
		require.Equal(t, "Bearer token", req.Header.Get("Authorization"))

		stream := `{"type":"message_delta","run_id":"run-1","job_id":"job-1","seq":1,"time_unix_ms":1,"data":{"delta":"DeepAgents runtime scaffold connected.","turn":1,"agent":null,"subagent":null}}
{"type":"done","run_id":"run-1","job_id":"job-1","seq":2,"time_unix_ms":1,"data":{"summary":"ok","turns":1,"tool_calls":0}}
`
		return responseFromText(http.StatusOK, stream, map[string]string{
			"Content-Type": "application/x-ndjson",
		})
	})

	err := client.StreamRun(context.Background(), DeepAgentsRunRequest{
		SchemaVersion: "2026-06-09",
		RunID:         "run-1",
	}, func(evt DeepAgentsEvent) error {
		received = append(received, evt)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, received, 2)
	require.Equal(t, "message_delta", received[0].Type)
	require.Equal(t, "done", received[1].Type)
}

func TestStreamRunReturnsErrorForBadStatus(t *testing.T) {
	client := newFakeClient(t, func(req *http.Request) *http.Response {
		bad := `{"message":"unauthorized"}`
		return responseFromText(http.StatusUnauthorized, bad)
	})
	err := client.StreamRun(context.Background(), DeepAgentsRunRequest{RunID: "run-1"}, func(evt DeepAgentsEvent) error {
		return nil
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "unauthorized")
}

func TestCancelRunHitsEndpoint(t *testing.T) {
	called := false
	client := newFakeClient(t, func(req *http.Request) *http.Response {
		called = true
		require.Equal(t, http.MethodPost, req.Method)
		require.True(t, strings.HasPrefix(req.URL.Path, "/internal/v1/runs/run-2/cancel"))
		return responseFromText(http.StatusOK, `{"status":"cancelled"}`)
	})

	require.NoError(t, client.CancelRun(context.Background(), "run-2"))
	require.True(t, called)
}

package fly

import (
	"errors"
	"net"
	"os/exec"
	"testing"
)

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"fly 429", &APIError{Status: 429}, true},
		{"fly 503", &APIError{Status: 503}, true},
		{"fly 502", &APIError{Status: 502}, true},
		{"fly 500", &APIError{Status: 500}, true},
		{"fly 400 bad request", &APIError{Status: 400}, false},
		{"fly 404", &APIError{Status: 404}, false},
		{"fly 422 invalid spec", &APIError{Status: 422}, false},
		{"network timeout", &net.OpError{Op: "dial", Err: errors.New("i/o timeout")}, true},
		{"flyctl exec error", &exec.ExitError{ProcessState: nil, Stderr: nil}, true},
		{"plain error", errors.New("oops"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsRetryable(c.err); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

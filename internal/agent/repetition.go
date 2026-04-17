package agent

import (
	"strings"
	"sync"
	"sync/atomic"
)

// repetitionDetector watches narration chunks for signs that the model is
// looping — generating the same line over and over, which is how the
// max_tokens → continue → max_tokens dance typically manifests when the model
// gets stuck dumping repeated output.
//
// It's line-based to stay cheap and to tolerate small drift (trailing
// whitespace, slightly varying indentation). Lines shorter than
// minLineLenForTracking are ignored so bullet markers, blank lines, and
// short boilerplate don't trip the detector.
const (
	minLineLenForTracking  = 40
	repetitionLoopThreshold = 8
)

type repetitionDetector struct {
	mu      sync.Mutex
	partial strings.Builder
	counts  map[string]int
	tripped atomic.Bool
}

func newRepetitionDetector() *repetitionDetector {
	return &repetitionDetector{counts: make(map[string]int)}
}

// Feed adds a narration chunk. Splits on newlines; only fully-terminated
// lines are counted (partial last line is held until the next Feed).
func (r *repetitionDetector) Feed(chunk string) {
	if chunk == "" || r.tripped.Load() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.partial.WriteString(chunk)
	s := r.partial.String()
	lastNL := strings.LastIndex(s, "\n")
	if lastNL < 0 {
		return
	}
	complete := s[:lastNL]
	tail := s[lastNL+1:]
	r.partial.Reset()
	r.partial.WriteString(tail)
	for _, line := range strings.Split(complete, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < minLineLenForTracking {
			continue
		}
		r.counts[trimmed]++
		if r.counts[trimmed] >= repetitionLoopThreshold {
			r.tripped.Store(true)
			return
		}
	}
}

// Tripped reports whether the repetition threshold has been crossed.
func (r *repetitionDetector) Tripped() bool {
	return r.tripped.Load()
}

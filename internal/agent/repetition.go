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
// very short boilerplate fragments don't trip the detector on common
// terse one-off outputs.
//
// Tuning (fix 6, trace 7d295609-a32a-4606-a3e3-4cf20c786229): lowered
// minLineLenForTracking from 40→20 (in the requested 15-25 range).
// The old cutoff let short repeated narrations through (e.g. the
// production "Dosya içeriğini yeniden okuyayım." at ~36 bytes, plus
// EN/FR/ES/etc. equivalents of re-read intent in the 25-35 byte range).
// This sustained the re-read-the-file loop until token budget or manual
// cancel. Threshold 20 catches semantically repetitive short full
// sentences indicating the stuck pattern while still suppressing pure
// boilerplate <18 bytes (bullets, "OK.", fragments). The line-based
// trimmed exact-string counting is unchanged, so normal short-but-unique
// narration never hits repetitionLoopThreshold=8. No secondary
// allowlist added in this minimal change (optional path not taken).
const (
	minLineLenForTracking  = 20
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

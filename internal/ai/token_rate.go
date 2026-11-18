package ai

// Minimums for reporting an output speed at all.
//
// A rate is only meaningful when there was an actual streaming window and a
// non-trivial sample. Backends that do not stream (a CLI printing the finished
// answer in one event) yield a near-zero GenMs; a very short reply yields a
// rate dominated by one pause. In both cases the honest answer is "not
// measured" — the UI hides the row rather than inventing a number.
const (
	minRateGenMs      = 500
	minRateOutputToks = 10
)

// DeriveTokensPerSecond computes the model's output speed for one turn.
//
// The denominator is GenMs — the time the model spent emitting text/thinking —
// not wall time. A turn that ran a 20-minute build produced few tokens while
// "talking", and dividing by the wall clock would report the build as model
// slowness.
//
// Returns ok=false when the turn has no usable measurement, in which case the
// caller must leave TokensPerSecond at zero.
func DeriveTokensPerSecond(outputTokens, genMs int) (float64, bool) {
	if outputTokens < minRateOutputToks || genMs < minRateGenMs {
		return 0, false
	}
	return float64(outputTokens) / (float64(genMs) / 1000), true
}

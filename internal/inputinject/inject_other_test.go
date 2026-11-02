//go:build !windows

package inputinject

import (
	"errors"
	"testing"
)

// TestApply_UnsupportedOnThisPlatform pins the stub: the helper binary is only
// shipped for Windows, so everywhere else Apply must fail loudly rather than
// silently accept an event the user would never see applied.
func TestApply_UnsupportedOnThisPlatform(t *testing.T) {
	if Supported() {
		t.Fatal("this build has no injector, so Supported must report false")
	}

	err := Apply(Event{Kind: "move", X: 0.5, Y: 0.5})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Apply = %v, want ErrUnsupported", err)
	}
}

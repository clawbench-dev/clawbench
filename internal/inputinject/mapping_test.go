package inputinject

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A 3840x2160 virtual desktop made of two 1920x1080 monitors side by side.
const (
	vdW = 3840
	vdH = 2160
)

func TestAbsolutePointer_WithoutBoundsCoversTheWholeDesktop(t *testing.T) {
	// Legacy behaviour: no display rectangle means the fraction spans every
	// monitor. This is what keeps a capture that reports no display working.
	dx, dy, err := AbsolutePointer(Event{X: 0.5, Y: 0.5}, 0, 0, vdW, vdH)
	require.NoError(t, err)
	assert.InDelta(t, 32767, dx, 1)
	assert.InDelta(t, 32767, dy, 1)

	dx, dy, err = AbsolutePointer(Event{X: 0, Y: 1}, 0, 0, vdW, vdH)
	require.NoError(t, err)
	assert.Equal(t, int32(0), dx)
	assert.Equal(t, int32(65535), dy)
}

func TestAbsolutePointer_MapsInsideTheCapturedDisplay(t *testing.T) {
	// Screen 2 starts at x=1920. Its centre must be the centre of THAT monitor
	// (2880 physical px), not the centre of the desktop (1920).
	bounds := &Rect{Left: 1920, Top: 0, Width: 1920, Height: 1080}
	dx, _, err := AbsolutePointer(Event{X: 0.5, Y: 0.5, Bounds: bounds}, 0, 0, vdW, vdH)
	require.NoError(t, err)
	assert.Equal(t, int32(2880*65535/vdW), dx)

	// The left edge of the captured display is the left edge of that monitor.
	dx, _, err = AbsolutePointer(Event{X: 0, Y: 0, Bounds: bounds}, 0, 0, vdW, vdH)
	require.NoError(t, err)
	assert.Equal(t, int32(1920*65535/vdW), dx)

	// A single-monitor host: the picture covers the whole desktop, so the result
	// is identical to the no-bounds case.
	dx, dy, err := AbsolutePointer(
		Event{X: 0.5, Y: 0.5, Bounds: &Rect{Width: vdW, Height: vdH}}, 0, 0, vdW, vdH)
	require.NoError(t, err)
	assert.InDelta(t, 32767, dx, 1)
	assert.InDelta(t, 32767, dy, 1)
}

func TestAbsolutePointer_HandlesNegativeVirtualOrigin(t *testing.T) {
	// A monitor placed left of the primary one makes the virtual origin
	// negative; VIRTUALDESK coordinates are relative to it, so the offset has
	// to be subtracted rather than assumed to be zero.
	bounds := &Rect{Left: -1920, Top: 0, Width: 1920, Height: 1080}
	dx, _, err := AbsolutePointer(Event{X: 0, Y: 0, Bounds: bounds}, -1920, 0, vdW, vdH)
	require.NoError(t, err)
	assert.Equal(t, int32(0), dx)

	dx, _, err = AbsolutePointer(Event{X: 1, Y: 0, Bounds: bounds}, -1920, 0, vdW, vdH)
	require.NoError(t, err)
	assert.Equal(t, int32(1920*65535/vdW), dx)
}

func TestAbsolutePointer_ClampsInsteadOfWrapping(t *testing.T) {
	// A drag that leaves the picture reports 0..1 (the renderer clamps), but a
	// rectangle that extends past the desktop must still not wrap around.
	bounds := &Rect{Left: 3000, Top: 0, Width: 2000, Height: 1080}
	dx, _, err := AbsolutePointer(Event{X: 1, Y: 0, Bounds: bounds}, 0, 0, vdW, vdH)
	require.NoError(t, err)
	assert.Equal(t, int32(65535), dx)
}

func TestAbsolutePointer_RejectsDegenerateInput(t *testing.T) {
	_, _, err := AbsolutePointer(Event{X: 0.5, Y: 0.5}, 0, 0, 0, vdH)
	assert.Error(t, err, "a zero-width desktop has no coordinate space")

	_, _, err = AbsolutePointer(Event{X: 0.5, Y: 0.5, Bounds: &Rect{Width: 0, Height: 1080}}, 0, 0, vdW, vdH)
	assert.Error(t, err, "a zero-width display rectangle would divide by zero")
}

func TestParseLine_AcceptsAndValidatesBounds(t *testing.T) {
	ev, err := ParseLine(`{"kind":"down","x":0.5,"y":0.5,"bounds":{"left":1920,"top":0,"width":1920,"height":1080}}`)
	require.NoError(t, err)
	require.NotNil(t, ev.Bounds)
	assert.Equal(t, 1920.0, ev.Bounds.Left)

	// A degenerate rectangle must be rejected at the boundary, not deep in the
	// syscall path where the failure is invisible.
	_, err = ParseLine(`{"kind":"down","x":0.5,"y":0.5,"bounds":{"left":0,"top":0,"width":0,"height":0}}`)
	assert.Error(t, err)
}

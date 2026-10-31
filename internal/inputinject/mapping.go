package inputinject

import "fmt"

// Rect is the physical-pixel rectangle of the captured picture on the virtual
// desktop.
//
// A screen share captures ONE display, but the pointer is positioned across the
// whole virtual desktop. Without this rectangle the fraction of the picture is
// mapped over every monitor at once, so on a two-monitor host a click in the
// middle of "screen 2" lands on the seam between the displays. The renderer
// knows which display it captured (desktopCapturer reports it), so it sends the
// rectangle with the events.
type Rect struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// AbsolutePointer converts a normalised picture coordinate into the 0..65535
// range MOUSEEVENTF_ABSOLUTE expects when combined with MOUSEEVENTF_VIRTUALDESK.
//
// vx/vy/vw/vh are the virtual-screen origin and size in physical pixels.
// Without bounds the whole virtual desktop is the picture (the behaviour before
// per-display mapping existed), which keeps window-less captures working.
func AbsolutePointer(ev Event, vx, vy, vw, vh int32) (int32, int32, error) {
	if vw <= 0 || vh <= 0 {
		return 0, 0, fmt.Errorf("no virtual screen")
	}
	// Absolute position in physical pixels on the virtual desktop.
	absX := float64(vx) + ev.X*float64(vw)
	absY := float64(vy) + ev.Y*float64(vh)
	if b := ev.Bounds; b != nil {
		if b.Width <= 0 || b.Height <= 0 {
			return 0, 0, fmt.Errorf("invalid display bounds: %vx%v", b.Width, b.Height)
		}
		absX = b.Left + ev.X*b.Width
		absY = b.Top + ev.Y*b.Height
	}
	return toAbsolute(absX-float64(vx), float64(vw)), toAbsolute(absY-float64(vy), float64(vh)), nil
}

// toAbsolute scales an offset from the virtual-screen origin into 0..65535,
// clamping so a point just outside the picture still lands on the edge instead
// of wrapping to the opposite side.
func toAbsolute(offset, extent float64) int32 {
	v := int32(offset * 65535 / extent)
	if v < 0 {
		return 0
	}
	if v > 65535 {
		return 65535
	}
	return v
}

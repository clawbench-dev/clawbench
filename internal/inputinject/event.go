// Package inputinject applies remote-control events to the local desktop.
//
// WHY A SEPARATE BINARY
// ---------------------
// Electron has no built-in way to move the OS pointer: `sendInputEvent` only
// reaches our own windows. The usual answer is a native npm module (robotjs /
// nut.js), which means a compiled dependency in every desktop build — and the
// user asked for something that needs no extra install.
//
// So the injection lives in a small Go program that ships INSIDE the app and is
// spawned on demand. Pure syscall, no CGO: it cross-compiles for Windows from
// any machine, and the app only starts it while a viewer actually holds control.
//
// The helper reads newline-delimited JSON from stdin and writes one JSON line
// per event to stdout, so the Electron side can await a result per event
// instead of firing into the void.
package inputinject

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Event is one pointer or keyboard action, in the same shape the renderer sends.
type Event struct {
	// Kind is move | down | up | wheel | key | text.
	Kind string `json:"kind"`
	// X/Y are normalised to the shared picture (0..1) for pointer events.
	X float64 `json:"x"`
	Y float64 `json:"y"`
	// Button is 0 left, 1 middle, 2 right.
	Button int `json:"button"`
	// Delta is the wheel delta in "lines"; positive scrolls down.
	Delta int `json:"delta"`
	// Key is a named key ("Enter", "Backspace", "a", "F5", …) for kind=key.
	Key string `json:"key"`
	// Modifiers are held for the duration of the event: ctrl/shift/alt/meta.
	Modifiers []string `json:"modifiers"`
	// Text is literal text to type for kind=text.
	Text string `json:"text"`
}

// ErrUnsupported means this platform has no injector compiled in.
var ErrUnsupported = errors.New("input injection is not supported on this platform")

// maxTextRunes bounds one typed string so a stuck key cannot flood the desktop.
const maxTextRunes = 512

// ParseLine decodes one NDJSON command.
//
// Unknown kinds are rejected rather than ignored: a typo in the renderer should
// surface as a failed event, not as "the click did nothing".
func ParseLine(line string) (Event, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Event{}, errors.New("empty event")
	}
	var ev Event
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ev); err != nil {
		return Event{}, fmt.Errorf("invalid event: %w", err)
	}
	switch ev.Kind {
	case "move", "down", "up", "wheel":
		if ev.X < 0 || ev.X > 1 || ev.Y < 0 || ev.Y > 1 {
			return Event{}, fmt.Errorf("coordinates out of range: %v,%v", ev.X, ev.Y)
		}
	case "key":
		if _, ok := VirtualKey(ev.Key); !ok {
			return Event{}, fmt.Errorf("unknown key %q", ev.Key)
		}
	case "text":
		if ev.Text == "" {
			return Event{}, errors.New("empty text")
		}
		if runes := []rune(ev.Text); len(runes) > maxTextRunes {
			ev.Text = string(runes[:maxTextRunes])
		}
	default:
		return Event{}, fmt.Errorf("unknown kind %q", ev.Kind)
	}
	return ev, nil
}

// VirtualKey maps a key name to a Windows virtual-key code.
//
// Named keys are case-insensitive; single characters map through their ASCII
// value, which is what the Windows API expects for letters and digits. Anything
// else is typed as literal text (see `kind:"text"`), so this table only has to
// cover the keys a remote-control user actually presses.
func VirtualKey(name string) (uint16, bool) {
	if name == "" {
		return 0, false
	}
	lower := strings.ToLower(name)
	if vk, ok := namedKeys[lower]; ok {
		return vk, true
	}
	// A single ASCII letter/digit: VK codes for these equal the ASCII value.
	if len(name) == 1 {
		c := name[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint16(c - 'a' + 'A'), true
		case c >= 'A' && c <= 'Z':
			return uint16(c), true
		case c >= '0' && c <= '9':
			return uint16(c), true
		}
	}
	return 0, false
}

var namedKeys = map[string]uint16{
	"backspace":  0x08,
	"tab":        0x09,
	"enter":      0x0D,
	"return":     0x0D,
	"shift":      0x10,
	"ctrl":       0x11,
	"control":    0x11,
	"alt":        0x12,
	"pause":      0x13,
	"capslock":   0x14,
	"esc":        0x1B,
	"escape":     0x1B,
	"space":      0x20,
	"pageup":     0x21,
	"pagedown":   0x22,
	"end":        0x23,
	"home":       0x24,
	"left":       0x25,
	"up":         0x26,
	"right":      0x27,
	"down":       0x28,
	"insert":     0x2D,
	"delete":     0x2E,
	"del":        0x2E,
	"meta":       0x5B,
	"win":        0x5B,
	"f1":         0x70,
	"f2":         0x71,
	"f3":         0x72,
	"f4":         0x73,
	"f5":         0x74,
	"f6":         0x75,
	"f7":         0x76,
	"f8":         0x77,
	"f9":         0x78,
	"f10":        0x79,
	"f11":        0x7A,
	"f12":        0x7B,
	"numlock":    0x90,
	"scrolllock": 0x91,
}

// ModifierKey maps a modifier name to its virtual-key code.
func ModifierKey(name string) (uint16, bool) {
	switch strings.ToLower(name) {
	case "ctrl", "control":
		return 0x11, true
	case "shift":
		return 0x10, true
	case "alt":
		return 0x12, true
	case "meta", "win", "cmd":
		return 0x5B, true
	}
	return 0, false
}

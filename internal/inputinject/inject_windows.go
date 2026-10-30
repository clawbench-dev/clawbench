//go:build windows

package inputinject

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// SendInput via user32, with no CGO and no native module.
//
// The INPUT struct is built by hand into a 32-byte union: Go's struct layout
// would work too, but hand-packing makes the offsets explicit and keeps the
// binary identical no matter which Go version compiles it.
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSendInput        = user32.NewProc("SendInput")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseMove        = 0x0001
	mouseLeftDown    = 0x0002
	mouseLeftUp      = 0x0004
	mouseRightDown   = 0x0008
	mouseRightUp     = 0x0010
	mouseMidDown     = 0x0020
	mouseMidUp       = 0x0040
	mouseWheel       = 0x0800
	mouseAbsolute    = 0x8000
	mouseVirtualDesk = 0x4000

	keyUp      = 0x0002
	keyUnicode = 0x0004

	// One wheel notch as reported by Chromium's DOM_WHEEL (deltaMode 0).
	domWheelNotch = 100.0
	wheelDelta    = 120

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCxVirtualScreen = 78
	smCyVirtualScreen = 79
)

// winInput mirrors INPUT: a DWORD type, padding, then a 32-byte union.
type winInput struct {
	typ  uint32
	_    uint32
	data [32]byte
}

func mouseInput(dx, dy int32, data uint32, flags uint32) winInput {
	in := winInput{typ: inputMouse}
	binary.LittleEndian.PutUint32(in.data[0:], uint32(dx))
	binary.LittleEndian.PutUint32(in.data[4:], uint32(dy))
	binary.LittleEndian.PutUint32(in.data[8:], data)
	binary.LittleEndian.PutUint32(in.data[12:], flags)
	// time and dwExtraInfo stay zero: the system fills them in.
	return in
}

func keyInput(vk, scan uint16, flags uint32) winInput {
	in := winInput{typ: inputKeyboard}
	binary.LittleEndian.PutUint16(in.data[0:], vk)
	binary.LittleEndian.PutUint16(in.data[2:], scan)
	binary.LittleEndian.PutUint32(in.data[4:], flags)
	return in
}

func systemMetric(index uintptr) int32 {
	v, _, _ := procGetSystemMetrics.Call(index)
	return int32(v)
}

// virtualScreen reports the bounding box of ALL monitors.
//
// Absolute mouse coordinates cover the virtual desktop only when the event also
// carries MOUSEEVENTF_VIRTUALDESK; without it every click lands on the primary
// monitor, scaled — which silently misplaces every click when the shared screen
// is a second display.
func virtualScreen() (x, y, cx, cy int32) {
	return systemMetric(smXVirtualScreen), systemMetric(smYVirtualScreen),
		systemMetric(smCxVirtualScreen), systemMetric(smCyVirtualScreen)
}

func send(inputs []winInput) error {
	if len(inputs) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if n == 0 {
		// Almost always UIPI: the target window runs elevated and we do not.
		return fmt.Errorf("SendInput rejected the event: %v", err)
	}
	return nil
}

func buttonFlags(button int, down bool) (uint32, bool) {
	switch button {
	case 0:
		if down {
			return mouseLeftDown, true
		}
		return mouseLeftUp, true
	case 1:
		if down {
			return mouseMidDown, true
		}
		return mouseMidUp, true
	case 2:
		if down {
			return mouseRightDown, true
		}
		return mouseRightUp, true
	}
	return 0, false
}

// Apply injects one event.
func Apply(ev Event) error {
	switch ev.Kind {
	case "move", "down", "up":
		// The normalised coordinate describes the CAPTURED PICTURE. For a screen
		// capture that picture is a monitor, so the fraction is mapped over the
		// whole virtual desktop (all monitors) rather than the primary one.
		// Window capture is refused by the UI, which is why no window offset is
		// applied here.
		vx, vy, vw, vh := virtualScreen()
		if vw <= 0 || vh <= 0 {
			return fmt.Errorf("no virtual screen")
		}
		// MOUSEEVENTF_ABSOLUTE uses 0..65535 across the virtual desktop; the
		// origin offset is applied by VIRTUALDESK itself.
		_ = vx
		_ = vy
		dx := int32(ev.X * 65535)
		dy := int32(ev.Y * 65535)
		flags := uint32(mouseMove | mouseAbsolute | mouseVirtualDesk)
		if ev.Kind == "down" || ev.Kind == "up" {
			btn, ok := buttonFlags(ev.Button, ev.Kind == "down")
			if !ok {
				return fmt.Errorf("unsupported button %d", ev.Button)
			}
			flags |= btn
		}
		return send([]winInput{mouseInput(dx, dy, 0, flags)})

	case "wheel":
		if ev.Delta == 0 {
			return nil
		}
		// Browser delta is in pixels (≈100 per notch), Windows wants notches of
		// 120, and the sign is inverted: DOM positive = down, WM_MOUSEWHEEL
		// positive = up.
		notches := -ev.Delta / domWheelNotch
		if notches > 10 {
			notches = 10
		} else if notches < -10 {
			notches = -10
		}
		amount := int32(notches * wheelDelta)
		if amount == 0 {
			// A sub-notch delta still has to move something, otherwise a slow
			// trackpad scroll does nothing at all.
			if ev.Delta > 0 {
				amount = -wheelDelta
			} else {
				amount = wheelDelta
			}
		}
		return send([]winInput{mouseInput(0, 0, uint32(amount), mouseWheel)})

	case "key":
		vk, ok := VirtualKey(ev.Key)
		if !ok {
			return fmt.Errorf("unknown key %q", ev.Key)
		}
		var inputs []winInput
		var pressed []uint16
		for _, m := range ev.Modifiers {
			if mvk, ok := ModifierKey(m); ok {
				inputs = append(inputs, keyInput(mvk, 0, 0))
				pressed = append(pressed, mvk)
			}
		}
		inputs = append(inputs, keyInput(vk, 0, 0), keyInput(vk, 0, keyUp))
		for i := len(pressed) - 1; i >= 0; i-- {
			inputs = append(inputs, keyInput(pressed[i], 0, keyUp))
		}
		return send(inputs)

	case "text":
		var inputs []winInput
		for _, unit := range utf16.Encode([]rune(ev.Text)) {
			inputs = append(inputs,
				keyInput(0, unit, keyUnicode),
				keyInput(0, unit, keyUnicode|keyUp),
			)
		}
		return send(inputs)
	}
	return fmt.Errorf("unknown kind %q", ev.Kind)
}

// Supported reports whether this build can inject input at all.
func Supported() bool {
	return true
}

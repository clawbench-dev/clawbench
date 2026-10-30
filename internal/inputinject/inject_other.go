//go:build !windows

package inputinject

// Apply is a stub on platforms without an injector.
//
// The helper binary is only shipped for Windows (the only platform this app
// shares a screen from), and building it here keeps `go build ./...` green
// everywhere without pretending the feature works.
func Apply(Event) error {
	return ErrUnsupported
}

// Supported reports whether this build can inject input at all.
func Supported() bool {
	return false
}

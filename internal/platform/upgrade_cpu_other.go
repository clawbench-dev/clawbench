//go:build !(linux && amd64)

package platform

// hostAMD64Features is only meaningful on a Linux amd64 host; elsewhere there
// is nothing to compare a downloaded amd64 binary against.
func hostAMD64Features() ([]string, bool) { return nil, false }

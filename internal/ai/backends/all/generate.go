// Package all imports every backend sub-package for its registration side
// effect. Import it (blank) instead of listing the sub-packages by hand.
package all

//go:generate go run ../../../cmd/genbackendimports

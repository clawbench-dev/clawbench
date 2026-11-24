//go:build !linux

package cli

import (
	"fmt"
	"os"
	"runtime"
)

// RunServiceCommand exists on every platform so main.go can dispatch
// unconditionally; only Linux has systemd, so everywhere else it explains that
// and exits non-zero rather than silently doing nothing.
func RunServiceCommand(_ []string) int {
	fmt.Fprintf(os.Stderr, "clawbench service: 只支持 Linux（当前系统是 %s）。\n", runtime.GOOS)
	return 1
}

package frp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"clawbench/internal/model"
)

// FindBinary searches for the frpc binary in priority order:
// 1. customPath (explicit user config)
// 2. PATH lookup
// 3. /usr/local/bin/frpc
// 4. model.DataDir/frpc
// Returns the absolute path or an error if not found.
func FindBinary(customPath string) (string, error) {
	candidates := []string{}

	if customPath != "" {
		candidates = append(candidates, customPath)
	}

	// PATH lookup
	if p, err := exec.LookPath("frpc"); err == nil {
		candidates = append(candidates, p)
	}

	candidates = append(candidates,
		"/usr/local/bin/frpc",
		"/usr/bin/frpc",
	)

	// DataDir (canonical source, set via --data-dir or default)
	if model.DataDir != "" {
		candidates = append(candidates, filepath.Join(model.DataDir, "frpc"))
	}

	for _, p := range candidates {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			// On non-Windows, check executable bit
			if runtime.GOOS != "windows" {
				if info.Mode().Perm()&0111 == 0 {
					continue
				}
			}
			return abs, nil
		}
	}

	return "", fmt.Errorf("frpc binary not found (searched: %v); install from https://github.com/fatedier/frp/releases", candidates)
}

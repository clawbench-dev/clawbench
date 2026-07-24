//go:build windows

package service

import "os/exec"

func setCmdProcessGroup(_ *exec.Cmd) {
	// No Setpgid on Windows
}

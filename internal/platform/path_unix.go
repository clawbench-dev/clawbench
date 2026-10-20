//go:build !windows

package platform

// listWindowsDrives is a stub on non-Windows platforms.
// On Windows, this function enumerates available drive letters.
func listWindowsDrives() []string {
	return nil
}

// listWindowsNetworkShares is a stub on non-Windows platforms.
// On Windows, this function enumerates remembered SMB shares (UNC roots).
func listWindowsNetworkShares() []string {
	return nil
}

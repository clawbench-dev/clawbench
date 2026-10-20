//go:build windows

package platform

import (
	"math"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// listWindowsDrives enumerates available Windows drive letters by checking
// whether the root directory of each letter exists (A:\ through Z:\).
func listWindowsDrives() []string {
	var drives []string
	for _, letter := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		root := string(letter) + ":\\"
		if _, err := os.Stat(root); err == nil {
			drives = append(drives, root)
		}
	}
	if len(drives) == 0 {
		// Fallback: at least return C:\
		drives = []string{"C:\\"}
	}
	return drives
}

// networkEnumTimeout bounds share enumeration. A dead SMB provider can make
// WNetEnumResourceW block for a long time; the root list is built during
// startup, so an unbounded call would hang the whole server (same reasoning as
// handler.fileInfoWithTimeout for unresponsive network mounts).
//
// On timeout the worker goroutine is abandoned rather than cancelled — Win32
// gives us no way to interrupt it. That leaks one goroutine until the provider
// answers, which is strictly better than blocking startup.
const networkEnumTimeout = 2 * time.Second

// listWindowsNetworkShares returns the UNC roots (`\\server\share`) of network
// connections the current user has, skipping the ones already reachable
// through a mapped drive letter (those appear in listWindowsDrives already).
//
// Best effort: any failure yields no shares. The picker still works without
// them — a UNC path can be typed into the jump dialog, and IsPathUnderAnyRoot
// accepts any share path regardless of this list.
func listWindowsNetworkShares() []string {
	done := make(chan []string, 1)
	go func() { done <- enumNetworkResources() }()
	select {
	case shares := <-done:
		return shares
	case <-time.After(networkEnumTimeout):
		return nil
	}
}

const (
	resourceConnected  = 0x00000001
	resourcetypeDisk   = 0x00000001
	errorMoreData      = 234
	wnetEnumBufferSize = 16 * 1024
	// maxEnumRounds bounds the ERROR_MORE_DATA retry loop. Shares are few; this
	// only exists so a misbehaving provider cannot spin forever.
	maxEnumRounds = 16
)

// netResourceW mirrors the Win32 NETRESOURCEW layout. Field order and sizes are
// part of the ABI — the struct is filled by mpr.dll, so it must not be reordered.
type netResourceW struct {
	Scope       uint32
	Type        uint32
	DisplayType uint32
	Usage       uint32
	LocalName   *uint16
	RemoteName  *uint16
	Comment     *uint16
	Provider    *uint16
}

var (
	mprDLL               = windows.NewLazySystemDLL("mpr.dll")
	procWNetOpenEnumW    = mprDLL.NewProc("WNetOpenEnumW")
	procWNetEnumResource = mprDLL.NewProc("WNetEnumResourceW")
	procWNetCloseEnum    = mprDLL.NewProc("WNetCloseEnum")
)

// Win32 calls below pass pointers to Go memory into mpr.dll. That is safe here
// because every call is synchronous (the provider fills the buffer before
// returning) and the pointed-to values stay alive across the call via the
// locals in scope. gosec's G103 flags the mechanism itself, so the individual
// conversions carry a nolint rather than the whole function.
func enumNetworkResources() []string {
	var handle windows.Handle
	// nil NetResource + RESOURCE_CONNECTED enumerates the connections of the
	// current user (mapped and remembered), which is exactly the SMB set.
	ret, _, _ := procWNetOpenEnumW.Call(
		resourceConnected, resourcetypeDisk, 0, 0,
		uintptr(unsafe.Pointer(&handle)), //nolint:gosec // synchronous Win32 call, see above
	)
	if ret != 0 {
		return nil
	}
	defer func() { _, _, _ = procWNetCloseEnum.Call(uintptr(handle)) }()

	buf := make([]byte, wnetEnumBufferSize)
	var shares []string
	for range maxEnumRounds {
		if len(buf) > math.MaxUint32 {
			return shares // unreachable: buffers are provider-sized, not user-sized
		}
		count := uint32(0xFFFFFFFF) // 0xFFFFFFFF = enumerate everything available
		size := uint32(len(buf))    //nolint:gosec // bounded by the check above
		ret, _, _ := procWNetEnumResource.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&count)),  //nolint:gosec // synchronous Win32 call
			uintptr(unsafe.Pointer(&buf[0])), //nolint:gosec // synchronous Win32 call
			uintptr(unsafe.Pointer(&size)),   //nolint:gosec // synchronous Win32 call
		)
		if ret != 0 && ret != errorMoreData {
			return shares
		}
		shares = append(shares, parseNetResources(buf, count, size)...)
		if ret != errorMoreData {
			return shares
		}
		// ERROR_MORE_DATA: `size` is the buffer size the provider needs. Reusing
		// the same buffer would return MORE_DATA forever.
		if int(size) > len(buf) {
			buf = make([]byte, size)
		}
	}
	return shares
}

// parseNetResources walks the packed NETRESOURCEW array WNetEnumResourceW
// filled in buf. Entries are back-to-back at sizeof(NETRESOURCEW) strides and
// `filled` is the only trustworthy bound — a truncated buffer would otherwise
// be read past its end.
func parseNetResources(buf []byte, count, filled uint32) []string {
	entrySize := unsafe.Sizeof(netResourceW{})
	limit := uintptr(filled)
	if limit > uintptr(len(buf)) {
		limit = uintptr(len(buf))
	}
	var shares []string
	for i := range count {
		offset := uintptr(i) * entrySize
		if offset+entrySize > limit {
			break
		}
		nr := (*netResourceW)(unsafe.Pointer(&buf[offset])) //nolint:gosec // offset bounded by limit above
		if nr.RemoteName == nil {
			continue
		}
		// A mapped connection carries a drive letter in LocalName; those are
		// already listed by listWindowsDrives. Only unmounted (remembered)
		// shares need to be added, or the picker would show "Z:\" and
		// "\\nas\media" for the same thing.
		if nr.LocalName != nil && windows.UTF16PtrToString(nr.LocalName) != "" {
			continue
		}
		if root, ok := UNCShareRoot(windows.UTF16PtrToString(nr.RemoteName)); ok {
			shares = append(shares, root)
		}
	}
	return shares
}

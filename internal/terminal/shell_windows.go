//go:build windows

package terminal

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                         = windows.NewLazySystemDLL("kernel32.dll")
	procCreatePseudoConsole          = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole          = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole           = kernel32.NewProc("ClosePseudoConsole")
	procInitializeProcThreadAttrList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttr         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttrList     = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateProcessW               = kernel32.NewProc("CreateProcessW")
)

const (
	procThreadAttributePseudoConsole = 0x00020016
	pseudoConsoleInheritCursor       = 0x1
	extendedStartupInfoPresent       = 0x00080000
)

// uintptrFromBool converts a bool to uintptr for Windows API calls.
func uintptrFromBool(v bool) uintptr {
	if v {
		return 1
	}
	return 0
}

// startPTY starts a shell process attached to a Windows Pseudo Console (ConPTY).
// Returns the output pipe (for reading shell output), the command, the input
// pipe write end (for HandleInput), a resize function wrapping
// ResizePseudoConsole, a close function wrapping ClosePseudoConsole + pipe
// cleanup, and any error.
func startPTY(cwd string, cols, rows uint16) (outputFile *os.File, cmd *exec.Cmd, inputWrite *os.File, resizeFn func(uint16, uint16) error, closePty func(), err error) {
	shell := resolveShell()
	slog.Info(
		"terminal: starting ConPTY",
		slog.String("shell", shell),
		slog.String("cwd", cwd),
	)

	if _, err := exec.LookPath(shell); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("shell not found: %w", err)
	}

	if cols == 0 || rows == 0 {
		cols, rows = 80, 24
	}

	// Create input pipe: client writes → inWrite → ConPTY reads from inRead
	var inRead, inWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("create input pipe: %w", err)
	}

	// Create output pipe: ConPTY writes → outWrite → client reads from outRead
	var outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, nil, nil, nil, nil, fmt.Errorf("create output pipe: %w", err)
	}

	// Create pseudo console
	var hpc windows.Handle
	c := &coordinateWindows{X: int16(cols), Y: int16(rows)}
	r1, _, callErr := procCreatePseudoConsole.Call(
		uintptr(unsafe.Pointer(c)),
		uintptr(inRead),
		uintptr(outWrite),
		0,
		uintptr(unsafe.Pointer(&hpc)),
	)
	if r1 != 0 {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, nil, nil, nil, nil, fmt.Errorf("CreatePseudoConsole failed: %v", callErr)
	}

	// ConPTY owns inRead and outWrite — close our references
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)

	// Build STARTUPINFOEX with PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE
	siEx, attrBuf, err := newStartupInfoEx(hpc)
	if err != nil {
		closeConPTY(hpc)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, nil, nil, nil, nil, fmt.Errorf("create startup info: %w", err)
	}
	defer siExCleanup(siEx, attrBuf)

	// Build command line for CreateProcessW
	// Use cmd.exe /c or pwsh -NoProfile as appropriate
	// For a shell, run the shell directly
	appName, err := syscall.UTF16PtrFromString(shell)
	if err != nil {
		closeConPTY(hpc)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, nil, nil, nil, nil, fmt.Errorf("convert shell path: %w", err)
	}

	// Build command line
	cmdLineStr := `"` + shell + `"`
	cmdLine, err := syscall.UTF16PtrFromString(cmdLineStr)
	if err != nil {
		closeConPTY(hpc)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, nil, nil, nil, nil, fmt.Errorf("convert command line: %w", err)
	}

	// Set current directory
	var cwdPtr *uint16
	if cwd != "" {
		cwdPtr, err = syscall.UTF16PtrFromString(cwd)
		if err != nil {
			closeConPTY(hpc)
			windows.CloseHandle(inWrite)
			windows.CloseHandle(outRead)
			return nil, nil, nil, nil, nil, fmt.Errorf("convert cwd: %w", err)
		}
	}

	// Build environment block: inherit + TERM=xterm-256color + COLORTERM=truecolor
	envBlock := buildEnvBlock()

	// StartupInfoEx with PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE
	// siEx has the startup info + attribute list, the attribute list is in attrBuf
	var processInfo windows.ProcessInformation

	ret, _, callErr := procCreateProcessW.Call(
		uintptr(unsafe.Pointer(appName)), // lpApplicationName
		uintptr(unsafe.Pointer(cmdLine)), // lpCommandLine
		0,                                 // lpProcessAttributes
		0,                                 // lpThreadAttributes
		uintptrFromBool(true),             // bInheritHandles = TRUE (required for ConPTY pipes)
		extendedStartupInfoPresent | windows.CREATE_UNICODE_ENVIRONMENT, // dwCreationFlags
		uintptr(unsafe.Pointer(&envBlock[0])), // lpEnvironment
		uintptr(unsafe.Pointer(cwdPtr)),   // lpCurrentDirectory
		uintptr(unsafe.Pointer(siEx)),     // lpStartupInfo (as STARTUPINFOEX)
		uintptr(unsafe.Pointer(&processInfo)),
	)
	if ret == 0 {
		closeConPTY(hpc)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, nil, nil, nil, nil, fmt.Errorf("CreateProcessW failed: %v", callErr)
	}

	// Close thread handle — we only need the process handle
	windows.CloseHandle(processInfo.Thread)

	// Create a proper os.Process via FindProcess so cmd.Wait() and
	// cmd.Process.Kill() work correctly with os/exec's internals.
	proc, err := os.FindProcess(int(processInfo.ProcessId))
	if err != nil {
		windows.CloseHandle(processInfo.Process)
		closeConPTY(hpc)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, nil, nil, nil, nil, fmt.Errorf("FindProcess: %w", err)
	}

	// Close the original CreateProcessW handle — FindProcess opened
	// its own duplicate handle.
	windows.CloseHandle(processInfo.Process)

	cmd = exec.Command(shell)
	cmd.Dir = cwd
	cmd.Process = proc

	outFile := os.NewFile(uintptr(outRead), "conpty-out")
	inFile := os.NewFile(uintptr(inWrite), "conpty-in")

	resizeFn = func(c, r uint16) error {
		return resizeConPTY(hpc, c, r)
	}

	closePty = func() {
		closeConPTY(hpc)
		outFile.Close()
		inFile.Close()
	}

	return outFile, cmd, inFile, resizeFn, closePty, nil
}

// coordinateWindows matches the Windows COORD struct (SHORT X, SHORT Y).
type coordinateWindows struct {
	X int16
	Y int16
}

// startupInfoExW is the Go-side representation of the Windows STARTUPINFOEXW
// structure. StartupInfo is followed by lpAttributeList, a pointer to a
// PROC_THREAD_ATTRIBUTE_LIST allocated separately.
type startupInfoExW struct {
	si              windows.StartupInfo
	lpAttributeList uintptr // pointer to PROC_THREAD_ATTRIBUTE_LIST
}

// newStartupInfoEx allocates and initializes a STARTUPINFOEX with the given
// pseudo console handle as PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE.
// Returns a pointer to the startupInfoExW struct, the backing buffer for the
// attribute list (must be kept alive until process creation), and error.
func newStartupInfoEx(hpc windows.Handle) (siExPtr unsafe.Pointer, attrBuf []byte, err error) {
	// First call to get required size
	var attrListSize uintptr
	procInitializeProcThreadAttrList.Call(
		0,                                       // lpAttributeList
		1,                                       // dwAttributeCount (1 attribute)
		0,                                       // dwFlags
		uintptr(unsafe.Pointer(&attrListSize)),  // lpSize
	)
	if attrListSize == 0 {
		return nil, nil, fmt.Errorf("InitializeProcThreadAttributeList returned size 0")
	}

	// Allocate separate buffer for the attribute list
	attrBuf = make([]byte, attrListSize)

	// Allocate the startupInfoExW struct on the heap
	siEx := &startupInfoExW{}
	siEx.si.Flags = windows.STARTF_USESTDHANDLES
	siEx.lpAttributeList = uintptr(unsafe.Pointer(&attrBuf[0]))

	// Initialize the attribute list
	ret, _, callErr := procInitializeProcThreadAttrList.Call(
		uintptr(siEx.lpAttributeList),
		1,                              // dwAttributeCount
		0,                              // dwFlags
		uintptr(unsafe.Pointer(&attrListSize)),
	)
	if ret == 0 {
		return nil, nil, fmt.Errorf("InitializeProcThreadAttributeList failed: %v", callErr)
	}

	// Add the pseudo console attribute
	ret, _, callErr = procUpdateProcThreadAttr.Call(
		uintptr(siEx.lpAttributeList),
		0, // dwFlags
		procThreadAttributePseudoConsole,
		uintptr(unsafe.Pointer(&hpc)),
		unsafe.Sizeof(hpc),
		0, // lpPreviousValue
		0, // lpReturnSize
	)
	if ret == 0 {
		procDeleteProcThreadAttrList.Call(uintptr(siEx.lpAttributeList))
		return nil, nil, fmt.Errorf("UpdateProcThreadAttribute failed: %v", callErr)
	}

	// Set Cb to the total size of the struct passed to CreateProcessW
	// (including the lpAttributeList pointer, but not the attribute list data itself)
	siEx.si.Cb = uint32(unsafe.Sizeof(*siEx))

	return unsafe.Pointer(siEx), attrBuf, nil
}

// siExCleanup frees the attribute list allocated by newStartupInfoEx.
func siExCleanup(siExPtr unsafe.Pointer, attrBuf []byte) {
	if len(attrBuf) == 0 {
		return
	}
	attrListPtr := unsafe.Pointer(&attrBuf[0])
	procDeleteProcThreadAttrList.Call(uintptr(attrListPtr))
}

func resizeConPTY(hpc windows.Handle, cols, rows uint16) error {
	c := &coordinateWindows{X: int16(cols), Y: int16(rows)}
	r1, _, callErr := procResizePseudoConsole.Call(
		uintptr(hpc),
		uintptr(unsafe.Pointer(c)),
	)
	if r1 != 0 {
		return fmt.Errorf("ResizePseudoConsole failed: %v", callErr)
	}
	return nil
}

func closeConPTY(hpc windows.Handle) {
	if hpc != 0 {
		procClosePseudoConsole.Call(uintptr(hpc))
	}
}

// buildEnvBlock creates a Windows environment block (null-separated, double-null terminated)
// inheriting from the current process and adding TERM and COLORTERM.
func buildEnvBlock() []uint16 {
	env := windows.Environ()
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")

	var buf []uint16
	for _, e := range env {
		// Find = separator (first equals sign)
		eqIdx := strings.IndexByte(e, '=')
		if eqIdx <= 0 {
			continue
		}
		key := e[:eqIdx]
		value := e[eqIdx+1:]

		// Encode as UTF-16
		key16 := utf16FromString(key)
		value16 := utf16FromString(value)

		// Append key=value\0
		buf = append(buf, key16...)
		buf = append(buf, '=')
		buf = append(buf, value16...)
		buf = append(buf, 0)
	}
	// Double null terminator
	buf = append(buf, 0)

	return buf
}

// utf16FromString converts a Go string to a null-terminated UTF-16 slice (without terminator).
func utf16FromString(s string) []uint16 {
	// Simple ASCII-only conversion for env vars
	r := make([]uint16, len(s))
	for i, c := range s {
		r[i] = uint16(c)
	}
	return r
}

// killProcessGroupSig kills the process on Windows.
// Windows doesn't have POSIX process groups, so we just kill the process.
func killProcessGroupSig(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	cmd.Process.Kill()
}



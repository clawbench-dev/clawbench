package main

import (
	"io"
	"log/slog"
	"os"
)

// watchParentPipe exits this server when its stdin closes.
//
// Why stdin and not a signal: on Windows a process cannot send a catchable
// signal to another process, and the embedder (the desktop shell) has to be
// able to stop the server without knowing it is a Windows console app. A pipe
// it holds open is the portable answer, and it covers the case a graceful
// "please quit" HTTP call would not: when the shell crashes, the pipe's read
// end sees EOF all the same, so no server is left running with nobody to
// manage it.
//
// The read is started only in --exit-on-stdin-close mode. Without it, a server
// started from a terminal that is later closed would also see EOF and shut
// down — which is not what a user running `clawbench` by hand asked for (the
// usual outcome there is to keep serving until told otherwise).
func watchParentPipe(in io.Reader, onClose func()) {
	go func() {
		// A buffered read, in a loop: the owner normally never writes, so the
		// one Read blocks until the writer goes away. Looping rather than
		// treating any byte as the end matters because Close() only guarantees
		// already-written data is delivered — a shell that wrote a handshake
		// byte and then closed in the same breath would otherwise be read as
		// "still alive" and its exit never noticed.
		buf := make([]byte, 256)
		for {
			n, err := in.Read(buf)
			if err == nil && n > 0 {
				continue
			}
			// EOF on a clean close, an error on a torn-down pipe: both mean
			// nobody is looking after this server any more.
			if err != nil && err != io.EOF {
				slog.Info("stdin read ended, shutting down", slog.String("err", err.Error()))
			} else {
				slog.Info("stdin closed by owner, shutting down")
			}
			onClose()
			return
		}
	}()
}

// enableParentPipeExit starts the stdin watcher when the flag is present.
func enableParentPipeExit() {
	if !hasFlag("--exit-on-stdin-close") {
		return
	}
	watchParentPipe(os.Stdin, func() { requestShutdown() })
}

// Command input-helper applies remote-control events to this desktop.
//
// It is spawned by the Electron main process and speaks newline-delimited JSON
// on stdin/stdout, one reply per request:
//
//	{"kind":"move","x":0.5,"y":0.5}      → {"ok":true}
//	{"kind":"key","key":"Enter"}         → {"ok":true}
//	{"kind":"bogus"}                     → {"ok":false,"error":"unknown kind \"bogus\""}
//
// A line protocol (rather than one process per event) matters: spawning a
// process per mouse move would cap the pointer at a few updates per second.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"clawbench/internal/inputinject"
)

type reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func main() {
	os.Exit(serve(os.Args[1:], os.Stdin, os.Stdout))
}

// serve runs the helper for one invocation and returns the process exit code.
// main only wires the real args and streams to it, so the whole surface — the
// --check dispatch and the line loop — is reachable from a test.
func serve(args []string, in io.Reader, out io.Writer) int {
	// --check lets the desktop app ask "can this build inject input?" without
	// sending an event (and without moving the user's pointer).
	if len(args) > 0 && args[0] == "--check" {
		return check(out)
	}

	// A stdin failure ends the helper; the parent sees the pipe close and
	// respawns it, so the error itself needs no handling here.
	_ = run(in, out)
	return 0
}

// check answers "can this build inject input?" and returns the process exit
// code: 2 when the platform has no injector, 0 when it does.
//
// It must NOT call Apply: on Windows that would move the pointer to the corner
// just to answer "are you there?".
func check(out io.Writer) int {
	if !inputinject.Supported() {
		_, _ = fmt.Fprintln(out, `{"ok":false,"error":"unsupported"}`)
		return 2
	}
	_, _ = fmt.Fprintln(out, `{"ok":true}`)
	return 0
}

// run reads one JSON command per line and writes one JSON reply per command
// until in is exhausted. It returns the scanner error, if any — the only way it
// fails is a line longer than the buffer, which ends the helper.
func run(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	w := bufio.NewWriter(out)
	defer func() { _ = w.Flush() }()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		ev, err := inputinject.ParseLine(line)
		if err == nil {
			err = inputinject.Apply(ev)
		}
		res := reply{OK: err == nil}
		if err != nil {
			res.Error = err.Error()
		}
		data, _ := json.Marshal(res)
		_, _ = w.Write(data)
		_ = w.WriteByte('\n')
		_ = w.Flush()
	}
	return scanner.Err()
}

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
	"os"

	"clawbench/internal/inputinject"
)

type reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func main() {
	// --check lets the desktop app ask "can this build inject input?" without
	// sending an event (and without moving the user's pointer).
	if len(os.Args) > 1 && os.Args[1] == "--check" {
		// Must NOT call Apply: on Windows that would move the pointer to the
		// corner just to answer "are you there?".
		if !inputinject.Supported() {
			fmt.Println(`{"ok":false,"error":"unsupported"}`)
			os.Exit(2)
		}
		fmt.Println(`{"ok":true}`)
		return
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 256*1024)
	out := bufio.NewWriter(os.Stdout)
	defer func() { _ = out.Flush() }()

	for in.Scan() {
		line := in.Text()
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
		_, _ = out.Write(data)
		_ = out.WriteByte('\n')
		_ = out.Flush()
	}
}

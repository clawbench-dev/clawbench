// Command helper is the fixture MCP server used by internal/mcp's stdio tests.
//
// It is a separate main package, not a shell script, because the tests must run
// on Windows too: a `#!/bin/sh` fixture fails there with "%1 is not a valid
// Win32 application". The tests build this binary once in TestMain and point the
// probe at it.
//
// The mode arrives in MCP_PROBE_TEST_HELPER:
//
//	server     answer initialize + tools/list (2 tools)
//	notools    answer initialize; tools/list returns []
//	half       answer initialize; tools/list returns a JSON-RPC error
//	silent     stay alive, never speak MCP
//	die        print to stderr and exit 1
//	diequiet   exit 1 without printing anything
//	spawn      answer normally, but first start a CHILD that inherits
//	           stdout/stderr and sleeps far longer than the probe. This is the
//	           launcher shape (uvx/npx/pipx): killing the leader alone leaves
//	           the child holding the pipes open, so cmd.Wait blocks forever
//	spawnsilent  "spawn" plus "silent": the timeout path, where the pending
//	           read would never see EOF if only the leader were killed
//	holdpipes  the child spawned by "spawn": inherit the pipes, say nothing,
//	           sleep.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	os.Exit(run(os.Getenv("MCP_PROBE_TEST_HELPER")))
}

func run(mode string) int {
	switch mode {
	case "die":
		fmt.Fprintln(os.Stderr, "fatal: MCP_TOKEN is not set")
		return 1
	case "diequiet":
		return 1
	case "holdpipes":
		// Deliberately never exits: the probe must not wait for it.
		time.Sleep(60 * time.Second)
		return 0
	}

	if mode == "spawn" || mode == "spawnsilent" {
		spawnPipeHoldingChild()
	}

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer func() { _ = writer.Flush() }()

	// spawnsilent answers nothing at all, so the probe is still blocked reading
	// stdout when its deadline fires.
	silent := mode == "silent" || mode == "spawnsilent"

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0
		}
		if silent {
			// Starts, stays alive, but never speaks MCP.
			fmt.Fprintln(writer, "not json at all")
			_ = writer.Flush()
			continue
		}

		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			fmt.Fprintf(writer,
				`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"probe-test","version":"9.9"}}}`+"\n",
				req.ID)
		case "tools/list":
			switch mode {
			case "half":
				fmt.Fprintf(writer,
					`{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"tools/list unavailable"}}`+"\n",
					req.ID)
			case "notools":
				fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[]}}`+"\n", req.ID)
			default:
				fmt.Fprintf(writer,
					`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"alpha"},{"name":"beta"}]}}`+"\n",
					req.ID)
			}
		}
		_ = writer.Flush()
	}
}

// spawnPipeHoldingChild starts a copy of this binary in "holdpipes" mode with
// stdout/stderr inherited from us, exactly like a launcher's interpreter child.
//
// It is left unreaped on purpose: it must still be alive (and holding the
// pipes) when the probe finishes, otherwise the test proves nothing.
func spawnPipeHoldingChild() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot resolve own path:", err)
		return
	}
	child := exec.Command(self)
	child.Env = append(os.Environ(), "MCP_PROBE_TEST_HELPER=holdpipes")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot start pipe-holding child:", err)
	}
}

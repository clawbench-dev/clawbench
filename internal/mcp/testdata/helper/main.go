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
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
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
	}

	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer func() { _ = writer.Flush() }()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return 0
		}
		if mode == "silent" {
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

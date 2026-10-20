// Command mcpserver is the fixture MCP server used by the handler package's
// connectivity-test cases.
//
// It is a separate main package, not a shell script, because the suite runs on
// Windows too: a `#!/bin/sh` fixture fails there with "%1 is not a valid Win32
// application", which is exactly how TestServeConfigTest_MCPProbesFormValues
// broke. The test builds this binary once and points the probe at it.
//
// MCP_PROBE_TEST_HELPER selects the mode: server (2 tools), notools, half,
// silent, die, diequiet.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Getenv("MCP_PROBE_TEST_HELPER")))
}

// Identity is overridable so a test can assert that the SERVER'S answer (its
// name and tool names) reaches the user, not just that a handshake happened.
var (
	serverName = "probe-test"
	serverVer  = "9.9"
	toolNames  = []string{"alpha", "beta"}
)

func run(mode string) int {
	if v := os.Getenv("MCP_PROBE_SERVER_NAME"); v != "" {
		serverName = v
	}
	if v := os.Getenv("MCP_PROBE_SERVER_VERSION"); v != "" {
		serverVer = v
	}
	if v := os.Getenv("MCP_PROBE_TOOLS"); v != "" {
		toolNames = strings.Split(v, ",")
	}
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
				`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":%q,"version":%q}}}`+"\n",
				req.ID, serverName, serverVer)
		case "tools/list":
			switch mode {
			case "half":
				fmt.Fprintf(writer,
					`{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"tools/list unavailable"}}`+"\n",
					req.ID)
			case "notools":
				fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[]}}`+"\n", req.ID)
			default:
				quoted := make([]string, 0, len(toolNames))
				for _, n := range toolNames {
					quoted = append(quoted, fmt.Sprintf(`{"name":%q}`, n))
				}
				fmt.Fprintf(writer,
					`{"jsonrpc":"2.0","id":%d,"result":{"tools":[%s]}}`+"\n",
					req.ID, strings.Join(quoted, ","))
			}
		}
		_ = writer.Flush()
	}
}

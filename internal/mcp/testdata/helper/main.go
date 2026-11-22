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
//	callhang   answer initialize and tools/list; never reply to tools/call
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
	"strings"
	"time"
)

func main() {
	// MCP_PROBE_TEST_COUNT_FILE lets a test count how many times the server was
	// started, without relying on timing. One byte per start.
	if p := os.Getenv("MCP_PROBE_TEST_COUNT_FILE"); p != "" {
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString("x")
			_ = f.Close()
		}
	}
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
	listCalls := 0

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
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			listChanged := "false"
			if mode == "listchanged" {
				listChanged = "true"
			}
			fmt.Fprintf(writer,
				`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{"listChanged":%s}},"serverInfo":{"name":"probe-test","version":"9.9"}}}`+"\n",
				req.ID, listChanged)
		case "tools/list":
			switch mode {
			case "half":
				fmt.Fprintf(writer,
					`{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"tools/list unavailable"}}`+"\n",
					req.ID)
			case "notools":
				fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[]}}`+"\n", req.ID)
			default:
				listCalls++
				// listchanged grows its list on every call after the first, so a
				// test can prove the client actually re-listed instead of
				// reusing its cached copy.
				extra := ""
				if mode == "listchanged" && listCalls > 1 {
					extra = `,{"name":"gamma","description":"Added later."}`
				}
				fmt.Fprintf(writer,
					`{"jsonrpc":"2.0","id":%d,"result":{"tools":[`+
						`{"name":"alpha","description":"Echo the arguments back.","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}} ,`+
						`{"name":"beta","description":"Return text and an image.\nSecond line.","inputSchema":{"type":"object","properties":{"fail":{"type":"boolean"}}}}`+
						`%s`+
						`]}}`+"\n",
					req.ID, extra)
			}
		case "tools/call":
			if mode == "callhang" {
				// Deliberately silent: the client must time out (and send a
				// cancellation) instead of hanging forever.
				continue
			}
			handleToolCall(writer, req.ID, req.Params)
		}
		_ = writer.Flush()
	}
}

// handleToolCall answers the two tools this fixture publishes.
//
// alpha echoes its arguments; beta returns text plus a 1x1 PNG, and switches to
// an isError result when called with {"fail":true}. That is enough for the
// session tests to cover both content shapes without changing the tool count
// the probe tests pin.
func handleToolCall(w *bufio.Writer, id int, params json.RawMessage) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(params, &call)

	reply := func(result any) {
		b, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  result,
		})
		if err != nil {
			return
		}
		fmt.Fprintf(w, "%s\n", b)
	}

	switch call.Name {
	case "alpha":
		args := strings.TrimSpace(string(call.Arguments))
		if args == "" {
			args = "{}"
		}
		// Built through Marshal, never by hand: pasting raw arguments into a
		// JSON string produces unescaped quotes, which the client then drops as
		// malformed — the failure looks like a hang, not a bad fixture.
		reply(map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "alpha:" + args},
		}})
	case "beta":
		var args struct {
			Fail bool `json:"fail"`
		}
		_ = json.Unmarshal(call.Arguments, &args)
		if args.Fail {
			reply(map[string]any{
				"isError": true,
				"content": []any{map[string]any{"type": "text", "text": "beta failed on purpose"}},
			})
			return
		}
		reply(map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "beta text"},
			map[string]any{"type": "image", "data": onePixelPNG, "mimeType": "image/png"},
		}})
	default:
		b, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": -32602, "message": "unknown tool " + call.Name},
		})
		fmt.Fprintf(w, "%s\n", b)
	}
}

// onePixelPNG is a 1x1 transparent PNG, base64. Small enough to inline and
// valid enough that a decoder accepts it.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

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

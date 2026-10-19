package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mcpProbeBackend 描述一个待探测的 ACP 后端。
//
// Args 是完整的 spawn 命令。刻意与 internal/ai/backends/*/register.go 的
// AcpCommand 保持一致 —— 本包不能 import backends（backends import 本包，
// 会形成环），所以用源码守卫 TestMCPProbeBackendTable 钉住漂移。
//
// 本表放在**无 build tag** 的文件里，使 CI 的常规 `go test ./internal/ai/`
// 就能发现漂移；真正的探测在 acp_mcp_probe_integration_test.go（integration tag）。
type mcpProbeBackend struct {
	ID string
	// Args is the full spawn command (command + args).
	Args []string
	// CLI is the binary the adapter needs to reach its engine. Empty means
	// "only check Args[0]" (npx bridges whose engine is bundled).
	CLI string
}

// mcpProbeBackends covers every backend that declares an AcpCommand today.
var mcpProbeBackends = []mcpProbeBackend{
	{ID: "claude", Args: []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp@latest"}, CLI: "claude"},
	{ID: "codex", Args: []string{"npx", "-y", "@agentclientprotocol/codex-acp@latest"}, CLI: "codex"},
	{ID: "codebuddy", Args: []string{"codebuddy", "--acp"}, CLI: "codebuddy"},
	{ID: "qoder", Args: []string{"qodercli", "--acp"}, CLI: "qodercli"},
	{ID: "kimi", Args: []string{"kimi", "acp"}, CLI: "kimi"},
	{ID: "opencode", Args: []string{"opencode", "acp"}, CLI: "opencode"},
	{ID: "mimo", Args: []string{"mimo", "acp"}, CLI: "mimo"},
	{ID: "grok", Args: []string{"grok", "agent", "stdio"}, CLI: "grok"},
	{ID: "cursor", Args: []string{"cursor-agent", "acp"}, CLI: "cursor-agent"},
	{ID: "commandcode", Args: []string{"cmdc", "acp"}, CLI: "cmdc"},
	{ID: "hermes", Args: []string{"hermes", "acp"}, CLI: "hermes"},
	{ID: "dsh", Args: []string{"dsh", "--profile", "acp"}, CLI: "dsh"},
	{ID: "deepseek", Args: []string{"codewhale", "serve", "--acp"}, CLI: "codewhale"},
	{ID: "copilot", Args: []string{"copilot", "--acp"}, CLI: "copilot"},
	{ID: "pi", Args: []string{"npx", "-y", "pi-acp@latest"}, CLI: "pi"},
	{ID: "antigravity", Args: []string{"npx", "-y", "agy-acp@latest"}, CLI: "agy"},
	{ID: "zcode", Args: []string{"npx", "-y", "zcode-acp-server"}},
}

// TestMCPProbeBackendTable 是源码守卫：探针表里每个后端的 spawn 命令必须与
// backends/*/register.go 的 AcpCommand 逐字一致。
//
// 为什么需要它：探针硬编码命令是因为 import 环，而 AcpCommand 会随上游适配器
// 改名/升级而变（本仓已有多次，如 pi 换适配器、kimi 换子命令）。表一旦漂移，
// 探针会去 spawn 一个不存在的命令并 skip，看起来"这个后端不支持 MCP"，
// 而真相是探针本身过期了 —— 正是这类假结论会误导接入决策。
func TestMCPProbeBackendTable(t *testing.T) {
	sources := readBackendSources(t)

	require.NotEmpty(t, mcpProbeBackends, "probe backend table is empty")

	seen := make(map[string]bool, len(mcpProbeBackends))
	for _, b := range mcpProbeBackends {
		require.NotEmpty(t, b.ID, "backend entry with empty ID")
		require.False(t, seen[b.ID], "duplicate probe backend %q", b.ID)
		seen[b.ID] = true
		require.NotEmpty(t, b.Args, "%s: empty spawn command", b.ID)

		want := `AcpCommand:` + strings.Repeat(" ", 0) + ` "` + strings.Join(b.Args, " ") + `"`
		if !strings.Contains(sources, want) {
			// Tolerate gofmt's column alignment: `AcpCommand:` is padded before
			// the literal, so match on the command string alone as a fallback.
			if !strings.Contains(sources, `"`+strings.Join(b.Args, " ")+`"`) {
				t.Errorf("probe backend %q spawns %q, which no backend declares as AcpCommand — "+
					"the probe table has drifted from internal/ai/backends/*/register.go",
					b.ID, strings.Join(b.Args, " "))
			}
		}
	}

	// Reverse direction: a backend that gains ACP support must be added to the
	// probe, otherwise the coverage claim ("全部 ACP 后端") silently becomes false.
	for _, line := range strings.Split(sources, "\n") {
		idx := strings.Index(line, "AcpCommand:")
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len("AcpCommand:"):])
		quoted := firstQuoted(rest)
		if quoted == "" {
			continue
		}
		if !containsArgs(mcpProbeBackends, quoted) {
			t.Errorf("backend declares AcpCommand %q but no probe entry covers it — "+
				"add it to mcpProbeBackends", quoted)
		}
	}
}

// readBackendSources concatenates every non-test Go file under backends/.
func readBackendSources(t *testing.T) string {
	t.Helper()

	root := filepath.Join("backends")
	var sb strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sb.Write(data)
		sb.WriteByte('\n')
		return nil
	})
	require.NoError(t, err, "walk %s", root)
	require.NotEmpty(t, sb.String(), "no backend sources found — has the layout moved?")
	return sb.String()
}

// firstQuoted returns the contents of the first double-quoted string in s.
func firstQuoted(s string) string {
	start := strings.IndexByte(s, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(s[start+1:], '"')
	if end < 0 {
		return ""
	}
	return s[start+1 : start+1+end]
}

func containsArgs(table []mcpProbeBackend, cmd string) bool {
	for _, b := range table {
		if strings.Join(b.Args, " ") == cmd {
			return true
		}
	}
	return false
}

//go:build integration

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// CodeBuddy ACP — 图片 Read 被代理成文本（根因探针）
// ===========================================================================
//
// 现象：通过 ClawBench 的 ACP 连接让 CodeBuddy 用 Read 工具读一张图片时，图片
// 被当成二进制/文本读入（模型拿到乱码），而不是作为图片内容块送进模型；但用
// `codebuddy` CLI（非 ACP）做同样的事则正常。
//
// 源码定位（codebuddy-code 2.154.0，dist-server）：
//
//	1) ToolManager.tryAcpIntercept(toolName, acpInterceptor, ...)：
//	     case L3.READ: if (acpInterceptor.isSupportRead()) return acpInterceptor.callRead(...)
//	   isSupportRead() === capabilities.fs.readTextFile（即客户端 Initialize 时
//	   广告的能力位）。命中后**原生 ReadTool 完全不被执行**，包括它的图片分支。
//
//	2) AcpInterceptor.callRead() 是纯文本路径：它调用客户端
//	   fs/read_text_file，把返回的 content 按行加行号后作为文本工具结果返回，
//	   没有任何 isImageFile / readImageFile 分支。
//
//	3) 原生 ReadTool.execute()（CLI 模式走的路径）才有 isImageFile →
//	   readImageFile → {type:"image_url",...}，再由
//	   AcpUtils.convertToolResultValueToAcp 转成 ACP image content block。
//
//	4) ACP 的 fs/read_text_file 响应类型是 ReadTextFileResponse{content string}
//	   —— 协议上只能承载文本，无法携带图片。ClawBench 的 ReadTextFile 实现是
//	   os.ReadFile 后 string(b)，PNG 字节被当成字符串（JSON 序列化时非法字节
//	   变成 U+FFFD）回给 agent。
//
// 因此根因是「能力位广告」与「CodeBuddy 的拦截分支」组合：
//   广告 fs.readTextFile=true  →  CodeBuddy 拦截 Read  →  纯文本代理  →  图片变乱码。
//
// 本探针驱动真实 `codebuddy --acp`，对同一张仓库内图片跑两种能力位配置：
//
//	A. fs.readTextFile = true （ClawBench 当前行为）
//	   → 断言 agent 确实发来针对该图片的 fs/read_text_file 请求（即走了代理），
//	     且工具结果里**没有** image content block（证明图片没有作为图片送达）。
//
//	B. fs.readTextFile = false（候选修复）
//	   → 断言 agent **不再**为该图片发起 fs/read_text_file（即回落到原生
//	     ReadTool 的图片分支）；并记录工具结果是否出现 image content block。
//
// 运行：
//
//	go test -v -run TestCodebuddyACP_ImageRead -tags integration \
//	    -timeout 420s ./internal/ai/
//
// 需要本机安装 codebuddy CLI 且已登录。

// fsReadRecord 记录一次客户端 fs/read_text_file 调用。
type fsReadRecord struct {
	Path          string
	Line, Limit   *int
	ReturnedBytes int
}

// imageReadProbeClient 包装真实客户端：录制 fs/read_text_file 调用，并捕获
// SDK 投递的 typed SessionNotification（原始 wire 由 recordingReader/Writer 另存）。
type imageReadProbeClient struct {
	*ClawBenchACPClient

	mu       sync.Mutex
	reads    []fsReadRecord
	captured []acp.SessionNotification
}

func (c *imageReadProbeClient) ReadTextFile(ctx context.Context, p acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	resp, err := c.ClawBenchACPClient.ReadTextFile(ctx, p)
	c.mu.Lock()
	c.reads = append(c.reads, fsReadRecord{Path: p.Path, Line: p.Line, Limit: p.Limit, ReturnedBytes: len(resp.Content)})
	c.mu.Unlock()
	return resp, err
}

func (c *imageReadProbeClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	c.mu.Lock()
	c.captured = append(c.captured, n)
	c.mu.Unlock()
	return c.ClawBenchACPClient.SessionUpdate(ctx, n)
}

func (c *imageReadProbeClient) readRecords() []fsReadRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]fsReadRecord, len(c.reads))
	copy(out, c.reads)
	return out
}

func (c *imageReadProbeClient) notifications() []acp.SessionNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]acp.SessionNotification, len(c.captured))
	copy(out, c.captured)
	return out
}

// repoImageForTest walks up from cwd to the repo root (go.mod) and returns a
// stable, tracked image fixture inside the project.
func repoImageForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("repo root (go.mod) not found")
		}
		dir = parent
	}
	img := filepath.Join(dir, "assets", "logo-128.png")
	if _, err := os.Stat(img); err != nil {
		t.Skipf("fixture image not found: %s", img)
	}
	return img
}

// runCodebuddyImageReadProbe spawns a real `codebuddy --acp`, advertises the
// given capabilities, and asks the agent to Read the image with the Read tool.
func runCodebuddyImageReadProbe(t *testing.T, ctx context.Context, caps acp.ClientCapabilities, imagePath string) (*imageReadProbeClient, *acpRawTraffic, error) {
	t.Helper()

	cmd := exec.CommandContext(ctx, "codebuddy", "--acp")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	agentOut, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	agentIn, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	cmd.Stderr = nil // keep test output readable

	rec := &acpRawTraffic{}
	recOut := &recordingReader{src: agentOut, buf: &bytes.Buffer{}}
	recIn := &recordingWriter{dst: agentIn, buf: &bytes.Buffer{}}

	require.NoError(t, cmd.Start(), "spawn codebuddy --acp")
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	base := NewClawBenchACPClient()
	client := &imageReadProbeClient{ClawBenchACPClient: base}
	conn := acp.NewClientSideConnection(client, recIn, recOut)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
		ClientInfo:         &acp.Implementation{Name: "clawbench-image-probe", Version: "1.0.0"},
	})
	if err != nil {
		return client, rec, fmt.Errorf("initialize: %w", err)
	}

	workDir := acpTestWorkDir()
	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: workDir, McpServers: []acp.McpServer{}})
	if err != nil {
		return client, rec, fmt.Errorf("new_session: %w", err)
	}

	streamCh := make(chan StreamEvent, 512)
	base.RegisterSession(string(newResp.SessionId), streamCh)
	// Drain the stream so the prompt never blocks on a full channel.
	go func() {
		for range streamCh {
		}
	}()

	prompt := fmt.Sprintf("请使用 Read 工具读取这个图片文件：%s\n"+
		"必须使用 Read 工具（不要用 Bash、cat 或其它方式），然后告诉我这张图片的内容。", imagePath)

	promptCtx, promptCancel := context.WithTimeout(ctx, 240*time.Second)
	defer promptCancel()
	_, err = conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
	})

	rec.fromAgent = parseWireMessages(recOut.buf)
	rec.toAgent = parseWireMessages(recIn.buf)
	return client, rec, err
}

// imageReadsFor returns the fs/read_text_file calls targeting the fixture image.
func imageReadsFor(recs []fsReadRecord, imagePath string) []fsReadRecord {
	base := filepath.Base(imagePath)
	var out []fsReadRecord
	for _, r := range recs {
		if r.Path == imagePath || filepath.Base(r.Path) == base {
			out = append(out, r)
		}
	}
	return out
}

// imageProbeEvidence summarizes the terminal tool results of one probe run.
type imageProbeEvidence struct {
	// sawImageBlock is true when some tool result carried an ACP image content
	// block (i.e. the image actually reached the model as an image).
	sawImageBlock bool
	// readResultText is the concatenated text of terminal Read tool results
	// (line-numbered in the ACP form "   1→<content>").
	readResultText string
	// sawPNGMagicAsText is true when readResultText contains the PNG signature
	// decoded as text — the definitive mojibake signature of a binary image
	// having been read through the text-only path.
	sawPNGMagicAsText bool
}

// collectImageProbeEvidence logs every terminal tool_call_update and reports
// whether any carried an ACP image content block, plus the text of Read
// results so callers can assert on the binary-as-text corruption.
func collectImageProbeEvidence(t *testing.T, notifications []acp.SessionNotification) imageProbeEvidence {
	t.Helper()
	var ev imageProbeEvidence
	var readTexts []string
	for _, n := range notifications {
		tcu := n.Update.ToolCallUpdate
		if tcu == nil {
			continue
		}
		name, _ := tcu.Meta["codebuddy.ai/toolName"].(string)
		status := "<nil>"
		if tcu.Status != nil {
			status = string(*tcu.Status)
		}
		hasImage := false
		var texts []string
		for _, c := range tcu.Content {
			if c.Content == nil {
				continue
			}
			if img := c.Content.Content.Image; img != nil {
				hasImage = true
				t.Logf("  [tool %s status=%s] IMAGE BLOCK: mime=%s dataLen=%d uri=%v",
					name, status, img.MimeType, len(img.Data), img.Uri)
			}
			if txt := c.Content.Content.Text; txt != nil && txt.Text != "" {
				texts = append(texts, txt.Text)
			}
		}
		if tcu.RawOutput != nil {
			if b, err := json.Marshal(tcu.RawOutput); err == nil {
				t.Logf("  [tool %s status=%s] rawOutput: %s", name, status, truncate(string(b), 200))
			}
		}
		if hasImage {
			ev.sawImageBlock = true
		}
		if name == "Read" && len(texts) > 0 {
			joined := strings.Join(texts, "\n")
			t.Logf("  [tool Read status=%s] text (first 200 chars): %s", status, truncate(joined, 200))
			readTexts = append(readTexts, joined)
		}
	}
	ev.readResultText = strings.Join(readTexts, "\n")
	// The PNG signature is 0x89 'P' 'N' 'G'. When read as text it survives as
	// U+FFFD (replacement char, from the invalid 0x89 byte) followed by "PNG".
	// Requiring both makes a false positive from an unrelated "PNG" mention
	// unlikely, and an image block would never produce this shape at all.
	ev.sawPNGMagicAsText = strings.Contains(ev.readResultText, "PNG") &&
		strings.Contains(ev.readResultText, "\uFFFD")
	return ev
}

// TestCodebuddyACP_ImageRead_ProxiedAsText 验证根因：
// 广告 fs.readTextFile 会让 CodeBuddy 把 Read 图片代理成纯文本；隐藏该能力位
// 后 CodeBuddy 回落到原生 ReadTool 的图片分支。
func TestCodebuddyACP_ImageRead_ProxiedAsText(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	// The client's ReadTextFile enforces isPathAllowed against model.RootPaths.
	origRoots := model.RootPaths
	model.RootPaths = []string{"/"}
	t.Cleanup(func() { model.RootPaths = origRoots })

	// bypassPermissions is the user's default, but set auto-approve anyway so a
	// permission prompt can never wedge the probe.
	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	imagePath := repoImageForTest(t)
	t.Logf("fixture image: %s", imagePath)

	t.Run("readTextFile_advertised_current_behavior", func(t *testing.T) {
		ctx, cancel := contextWithTimeout(t, 300*time.Second)
		defer cancel()

		client, rec, err := runCodebuddyImageReadProbe(t, ctx, acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		}, imagePath)
		if err != nil {
			t.Logf("NOTE: prompt returned error: %v", err)
		}

		rec.dump(t)

		reads := imageReadsFor(client.readRecords(), imagePath)
		for _, r := range reads {
			t.Logf("client received fs/read_text_file: path=%s line=%v limit=%v returned=%dB",
				r.Path, r.Line, r.Limit, r.ReturnedBytes)
		}

		ev := collectImageProbeEvidence(t, client.notifications())

		// Hard precondition: the proxy is what breaks images. If the agent did
		// not call fs/read_text_file, this probe cannot conclude anything.
		require.NotEmpty(t, reads,
			"EXPECTED CodeBuddy to proxy the image Read through fs/read_text_file when "+
				"fs.readTextFile is advertised — see raw wire dump above")
		require.False(t, ev.sawImageBlock,
			"the proxied Read is text-only; an ACP image block must NOT appear when "+
				"fs.readTextFile is advertised (that is the bug)")
		require.True(t, ev.sawPNGMagicAsText,
			"expected the Read result to contain the PNG signature decoded as text "+
				"(U+FFFD + \"PNG\") — the mojibake signature of a binary image read "+
				"through the text-only proxy. Got: %q", truncate(ev.readResultText, 300))

		t.Logf("CONFIRMED: advertising fs.readTextFile routes image Read through the "+
			"text-only fs/read_text_file proxy (calls=%d). The client returned %d raw "+
			"bytes which CodeBuddy line-numbered and handed to the model as TEXT — the "+
			"PNG signature appears as U+FFFD+\"PNG\". No image content block was emitted.",
			len(reads), reads[0].ReturnedBytes)
	})

	t.Run("readTextFile_hidden_candidate_fix", func(t *testing.T) {
		ctx, cancel := contextWithTimeout(t, 300*time.Second)
		defer cancel()

		client, rec, err := runCodebuddyImageReadProbe(t, ctx, acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{ReadTextFile: false, WriteTextFile: true},
		}, imagePath)
		if err != nil {
			t.Logf("NOTE: prompt returned error: %v", err)
		}

		rec.dump(t)

		reads := imageReadsFor(client.readRecords(), imagePath)
		ev := collectImageProbeEvidence(t, client.notifications())

		require.Empty(t, reads,
			"with fs.readTextFile hidden, CodeBuddy must NOT proxy the image Read "+
				"through fs/read_text_file — see raw wire dump above")

		if ev.sawImageBlock {
			require.False(t, ev.sawPNGMagicAsText,
				"the image reached the model as an image block, so the Read result "+
					"must not also carry the raw PNG signature as text")
			t.Logf("CONFIRMED: hiding fs.readTextFile makes CodeBuddy fall back to its " +
				"native ReadTool, which emits an ACP image content block for the image.")
		} else {
			t.Logf("NOTE: no image block observed. The Read may have been routed to the " +
				"native tool but rejected (e.g. current model has supportsImages=false), " +
				"or the model did not call Read this run. Compare with the dumps above.")
		}
	})
}

// codebuddyImageReadACPAgent returns a CodeBuddy ACP agent for the end-to-end
// production-path test.
func codebuddyImageReadACPAgent() *model.Agent {
	return &model.Agent{
		ID:         "codebuddy-acp-image-read-test",
		Name:       "CodeBuddy ACP Image Read Test",
		Backend:    "codebuddy",
		Transport:  "acp-stdio",
		AcpCommand: "codebuddy --acp",
		Models:     []model.AgentModel{{ID: "deepseek-v4-flash", Name: "deepseek-v4-flash", Default: true}},
	}
}

// TestCodebuddyACP_ImageRead_ProductionPath_NoMojibake 端到端验证修复：
// 走生产 ACPBackend 路径（而非探针手工构造的能力位），确认 CodeBuddy 会话里
// Read 图片后，工具结果**不再**出现「PNG 魔数被当文本」的乱码特征。
//
// 这是修复的回归守卫：若有人把 fs.readTextFile 改回对所有 backend 广告，
// 本测试会因 PNG 魔数重新出现在 Read 文本里而失败。
func TestCodebuddyACP_ImageRead_ProductionPath_NoMojibake(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	origRoots := model.RootPaths
	model.RootPaths = []string{"/"}
	t.Cleanup(func() { model.RootPaths = origRoots })

	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	imagePath := repoImageForTest(t)

	agent := codebuddyImageReadACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	t.Cleanup(func() { env.closeConn(t, sessionID) })

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt: fmt.Sprintf("请使用 Read 工具读取这个图片文件：%s\n"+
			"必须使用 Read 工具（不要用 Bash、cat 或其它方式），然后告诉我这张图片的内容。", imagePath),
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
	})
	require.NoError(t, err)

	events := collectACPEvents(t, ch, 240*time.Second)

	// Concatenate every tool_result output for the Read tool. A proxied
	// (text) read leaves the PNG signature in this text; the native image
	// path never does.
	var readOutputs []string
	for _, e := range events {
		if e.Type != "tool_result" || e.Tool == nil || e.Tool.Name != "Read" {
			continue
		}
		t.Logf("Read tool_result: status=%q output(first 200)=%q",
			e.Tool.Status, truncate(e.Tool.Output, 200))
		readOutputs = append(readOutputs, e.Tool.Output)
	}

	if len(readOutputs) == 0 {
		t.Skipf("CodeBuddy did not invoke the Read tool this run; cannot assert on the "+
			"image path (fixture=%s). Re-run or check the model's tool use.", imagePath)
	}

	joined := strings.Join(readOutputs, "\n")
	require.False(t, strings.Contains(joined, "\uFFFD") && strings.Contains(joined, "PNG"),
		"REGRESSION: the Read tool result contains the PNG signature decoded as text "+
			"(U+FFFD + \"PNG\"), meaning image Reads are again being proxied through the "+
			"text-only fs/read_text_file path. fs.readTextFile must stay hidden for "+
			"CodeBuddy. Output: %q", truncate(joined, 300))

	t.Logf("CONFIRMED: production ACPBackend path no longer proxies image Reads as text "+
		"(read_results=%d)", len(readOutputs))
}

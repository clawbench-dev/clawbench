//go:build integration

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	// Deliberately NOT calling conn.SetLogger: the SDK writes c.logger without
	// synchronization while receive() reads it, so setting it after
	// construction is a data race that `-race` reports intermittently. See the
	// matching note in acp_conn_lifecycle.go spawnLocked. Diagnostics fall back
	// to slog.Default().

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
	sessionID := string(newResp.SessionId)
	base.RegisterSession(sessionID, streamCh)
	// Drain the stream so the prompt never blocks on a full channel. The
	// goroutine stops via a signal rather than by closing streamCh: the ACP
	// client may still hold the channel and forward events after
	// UnregisterSession, and a send on a closed channel panics. Production
	// never closes these channels either.
	drainStop := make(chan struct{})
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case <-streamCh:
			case <-drainStop:
				return
			}
		}
	}()
	t.Cleanup(func() {
		base.UnregisterSession(sessionID)
		close(drainStop)
		<-drainDone
	})

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
//
// Models is inert metadata here: ExecuteStream is called without req.Model, so
// CodeBuddy falls back to its own configured default model. The entry is kept
// only because the other CodeBuddy ACP test agents carry one.
func codebuddyImageReadACPAgent() *model.Agent {
	return &model.Agent{
		ID:         "codebuddy-acp-image-read-test",
		Name:       "CodeBuddy ACP Image Read Test",
		Backend:    "codebuddy",
		Transport:  "acp-stdio",
		AcpCommand: "codebuddy --acp",
		Models:     []model.AgentModel{{ID: "glm-4-plus", Name: "glm-4-plus", Default: true}},
	}
}

// runProductionImageRead drives one full turn through the production ACPBackend
// and returns the Read tool-result outputs for that turn.
func runProductionImageRead(t *testing.T, backend *ACPBackend, sessionID, imagePath string) []string {
	t.Helper()

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

	var readOutputs []string
	for _, e := range events {
		if e.Type != "tool_result" || e.Tool == nil || e.Tool.Name != "Read" {
			continue
		}
		t.Logf("Read tool_result: status=%q output(first 200)=%q",
			e.Tool.Status, truncate(e.Tool.Output, 200))
		readOutputs = append(readOutputs, e.Tool.Output)
	}
	return readOutputs
}

// TestCodebuddyACP_ImageRead_ProductionPath 端到端验证修复：
// 走生产 ACPBackend 路径（而非探针手工构造的能力位），确认 CodeBuddy 会话里
// Read 图片时，工具结果携带的是图片内容而不是「PNG 魔数被当文本」的乱码。
//
// 这是修复的回归守卫：若有人把 fs.readTextFile 改回对所有 backend 广告，
// Read 会重新走文本代理，本测试会因出现 U+FFFD+"PNG" 而失败。
//
// 注意本测试是**双向**断言：既要求出现图片载荷，也要求不出现乱码。只断言
// 「没有乱码」是不够的 —— Read 直接报错或返回空同样满足那个条件。
//
// 模型是否调用 Read 由模型决定，故用有限重试吸收偶发不配合；重试后仍未调用
// 则**失败而非跳过**（integration 测试不在 CI 中运行，skip 会让这个守卫永远
// 静默通过）。
func TestCodebuddyACP_ImageRead_ProductionPath(t *testing.T) {
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

	const attempts = 3
	var readOutputs []string
	for attempt := 1; attempt <= attempts; attempt++ {
		// A fresh session per attempt: a retry that reuses the previous session
		// would let the model answer from conversation history instead of
		// calling Read again.
		sessionID := acpSessionID()
		readOutputs = runProductionImageRead(t, backend, sessionID, imagePath)
		env.closeConn(t, sessionID)
		if len(readOutputs) > 0 {
			break
		}
		t.Logf("attempt %d/%d: CodeBuddy did not invoke the Read tool; retrying", attempt, attempts)
	}

	require.NotEmpty(t, readOutputs,
		"CodeBuddy never invoked the Read tool in %d attempts (fixture=%s). This guard "+
			"cannot conclude anything, so it fails rather than skipping — a silent skip "+
			"would leave the image-Read regression unguarded forever.", attempts, imagePath)

	joined := strings.Join(readOutputs, "\n")

	// Negative: no binary-as-text corruption.
	require.False(t, strings.Contains(joined, "\uFFFD") && strings.Contains(joined, "PNG"),
		"REGRESSION: the Read tool result contains the PNG signature decoded as text "+
			"(U+FFFD + \"PNG\"), meaning image Reads are again being proxied through the "+
			"text-only fs/read_text_file path. fs.readTextFile must stay hidden for "+
			"CodeBuddy. Output: %q", truncate(joined, 300))

	// Positive: the image actually arrived. On the native path CodeBuddy's Read
	// returns the image_url payload, which ClawBench surfaces in the tool
	// result — so a broken-but-quiet Read cannot satisfy this test.
	require.True(t, strings.Contains(joined, "image_url") && strings.Contains(joined, "data:image/"),
		"expected the Read tool result to carry the image payload (image_url with a "+
			"data: URI). A Read that returned an error or empty output would pass the "+
			"mojibake check alone, so this assertion is required to prove the image "+
			"reached the model. Output: %q", truncate(joined, 300))

	t.Logf("CONFIRMED: production ACPBackend path delivers the image payload and no "+
		"longer proxies image Reads as text (read_results=%d)", len(readOutputs))
}

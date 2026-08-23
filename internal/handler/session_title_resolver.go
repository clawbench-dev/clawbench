package handler

// ═════════════════════════════════════════════════════════════════════════════
// MODULE 2 — BACKEND-SPECIFIC TITLE OPTIMIZATION (per-agent enhancement
// over Module 1's universal cleaning; see session_resume.go). Today this
// ships the claude-code resolver: transcript reading for exact first
// questions on compacted sessions, custom-title lookup, and claude-native
// strip rules. Other backends plug in the same way — implement the four
// methods, register, done. Unregistered backends simply keep Module 1's
// universal behavior.
//
// 模块二——后端专属标题优化(在模块一的通用清洗之上做逐后端增强,见
// session_resume.go)。当前内置 claude-code 解析器:读转录以在压缩会话上取
// 精确首问、custom-title 查询、claude 原生剥离规则。其他后端以同样方式接入
// ——实现四个方法、注册即完成。未注册后端保持模块一的通用行为。
// ═════════════════════════════════════════════════════════════════════════════
//
// STANDARD INPUT BLOCK — decoupling contract between the session-title module
// and any agent backend's on-disk data structures.
//
// The title-fix module (tier order, capping, machine-title suppression) is
// backend-agnostic. Everything backend-specific is reduced to THIS interface:
// implement four methods, register under the backend name, and the whole
// module works for that backend — both the external session list display and
// the acp-load import path pick it up automatically.
//
// 标准输入块——会话标题模块与任意智能体后端磁盘数据结构的解耦契约。
// 标题模块本身（层级顺序、截断、机器标题抑制）与后端无关；所有后端相关的
// 部分被收敛为本接口：实现四个方法、按后端名注册，整个模块即对该后端生效
// ——外部会话列表展示与 acp-load 导入两条路径都会自动接入。
//
// To add a backend (e.g. codex, opencode):
//  1. Locate its transcript storage with REAL data (run the CLI, find where
//     session files appear; note the dir pattern and how cwd is encoded).
//  2. Implement the four methods below as a thin adapter over its path rules
//     and transcript schema, mirroring claudeTranscriptResolver.
//  3. Register it in sessionTranscriptResolvers.
// Only register backends verified against REAL transcripts — never ship a
// blind resolver (see the fuller guide above acpTranscriptPath in agent.go).
//
// 新增后端（如 codex、opencode）：
//  1. 用真实数据定位其转录存储（跑一次 CLI，找到会话文件位置；记下目录规律
//     与 cwd 编码方式）。
//  2. 按其路径规则与转录格式实现下方四方法的薄适配层，仿 claudeTranscriptResolver。
//  3. 在 sessionTranscriptResolvers 注册。
// 仅注册经真实转录验证过的后端——禁止提交未经验证的盲解析器（完整指南见
// agent.go 中 acpTranscriptPath 上方注释块）。

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// sessionTranscriptResolver is the standard input block: everything the
// title module needs to know about one backend's session storage.
//
// 会话转录解析器即标准输入块：标题模块需要知道的关于某后端会话存储的全部。
type sessionTranscriptResolver interface {
	// TranscriptPath locates the backend's transcript file for a session.
	// Return "" when no transcript exists. sessionID comes from the wire —
	// implementations MUST reject IDs carrying path separators, traversal
	// segments or glob metacharacters (see acpTranscriptPath).
	//
	// 定位该后端的会话转录文件；无则返回 ""。sessionID 来自网络输入——
	// 实现必须拒绝含路径分隔符、穿越段或 glob 元字符的 ID（见 acpTranscriptPath）。
	TranscriptPath(home, cwd, sessionID string) string

	// TitleCandidates extracts BOTH title candidates from the transcript in
	// ONE scan: the backend-persisted custom title (newest record, a user
	// rename or auto-generated topic name) and the first human-typed question
	// (machine prefixes stripped). Either may be "". Implementations should
	// cache results by (sessionID, modTime) so tier-1-miss → tier-2 lookups
	// cost one read, not two.
	//
	// 单次扫描同时提取两个标题候选：后端自持久化的标题（最新记录，用户改名
	// 或自动主题名）与第一条人类提问（剥机器前缀后）。任一可为 ""。实现应
	// 按 (sessionID, modTime) 缓存结果，使"第 1 层未命中→查第 2 层"只花一次
	// 读盘而非两次。
	TitleCandidates(path string) (customTitle, firstQuestion string)

	// CustomTitle returns the title the backend itself persisted for the
	// session (a user rename or auto-generated topic name), "" when none.
	// Convenience wrapper over TitleCandidates for callers needing one value —
	// it triggers the same single scan and discards the question.
	//
	// 返回后端自身为会话持久化的标题（用户改名或自动生成的主题名），无则 ""。
	// 单值场景下对 TitleCandidates 的便捷包装——同样触发单次扫描，丢弃首问。
	CustomTitle(path string) string

	// FirstQuestion returns the first human-typed message from the
	// transcript, with machine-generated prefixes stripped, "" when none.
	// Convenience wrapper over TitleCandidates for callers needing one value —
	// it triggers the same single scan and discards the custom title.
	//
	// 返回转录中第一条人类输入（剥离机器前缀后），无则 ""。单值场景下对
	// TitleCandidates 的便捷包装——同样触发单次扫描，丢弃自持久化标题。
	FirstQuestion(path string) string

	// StripRules returns THIS backend's native machine-prefix strip rules
	// (compaction headers, slash-command wrappers, local-command caveats,
	// ...). They add to clientInjectedStripRules (which the client prepends
	// for every backend) and feed the data-driven stripper. Harmless no-ops
	// for other backends.
	//
	// 返回该后端原生的机器前缀剥离规则（压缩头、斜杠命令包装、本地命令警示
	// 等）。叠加在客户端对所有后端注入的 clientInjectedStripRules 之上，喂给
	// 数据驱动剥离器。对其他后端为无害空转。
	StripRules() []stripRule
}

// sessionTranscriptResolvers maps backend name → resolver. Backends absent
// from this map fall back to the backend-agnostic ACP-replay derivation.
//
// 后端名 → 解析器注册表。未注册的后端回退到与后端无关的 ACP 重放派生。
var sessionTranscriptResolvers = map[string]sessionTranscriptResolver{
	"claude": claudeTranscriptResolver{},
}

// transcriptResolverFor looks up the resolver for a backend; nil when the
// backend has none registered.
//
// 按后端名查解析器；未注册返回 nil。
func transcriptResolverFor(backend string) sessionTranscriptResolver {
	return sessionTranscriptResolvers[backend]
}

// stripRulesFor returns the full strip-rule set for a backend: the
// client-injected universal rules plus the backend's native rules. The
// detector (isMachineGeneratedTitleFor) derives its flat prefix list from
// these rules too.
//
// 返回某后端的完整剥离规则集:客户端通用注入规则 + 该后端原生规则。检测器
// (isMachineGeneratedTitleFor) 的扁平前缀列表也由此推导。
func stripRulesFor(r sessionTranscriptResolver) []stripRule {
	rules := append([]stripRule{}, clientInjectedStripRules...)
	if r != nil {
		rules = append(rules, r.StripRules()...)
	}
	return rules
}

// machinePrefixesFor returns the flat prefix list used for title
// machine-text DETECTION, derived from stripRulesFor (prefixes only).
//
// 返回标题机器文本"检测"用的扁平前缀列表,由 stripRulesFor 推导(仅取前缀)。
func machinePrefixesFor(r sessionTranscriptResolver) []string {
	rules := stripRulesFor(r)
	prefixes := make([]string, len(rules))
	for i, rule := range rules {
		prefixes[i] = rule.prefix
	}
	return prefixes
}

// ═════════════════════════════════════════════════════════════════════════════
// claudeTranscriptResolver — the claude-code CLI's adapter.
//
// Implements the four resolver methods over ~/.claude/projects/ storage:
// transcript path resolution, custom-title extraction, first-question
// extraction, and claude-native strip rules.
// ═════════════════════════════════════════════════════════════════════════════

// claudeTranscriptResolver adapts the claude-code CLI's transcript storage
// for the session-title module.
//
// claude 转录解析器：适配 claude-code CLI 的转录存储。
type claudeTranscriptResolver struct{}

func (claudeTranscriptResolver) TranscriptPath(home, cwd, sessionID string) string {
	return resolveTranscriptPath(home, cwd, sessionID)
}

// TitleCandidates implements the claude resolver's single-scan extraction.
// It delegates to scanTranscriptForTitles, which streams the .jsonl transcript
// line by line and returns BOTH candidates in one pass: the newest
// "custom-title" record (tier 1) and the first real user question after
// machine-prefix stripping (tier 2). The scan honors the (sessionID, modTime)
// result cache, so a repeated call for the same unchanged file costs one stat
// + one map lookup instead of a re-read.
//
// 实现 claude 解析器的单次扫描提取：转调 scanTranscriptForTitles。该函数逐行
// 流式读取 .jsonl 转录，一次遍历同时返回两个候选——最新的 custom-title 记录
// （第 1 层）与剥离机器前缀后的第一条真实用户提问（第 2 层）。扫描遵循
// (sessionID, modTime) 结果缓存，因此对同一未变更文件的重复调用只花一次
// stat + 一次查表，不会重新读盘。
func (claudeTranscriptResolver) TitleCandidates(path string) (string, string) {
	return scanTranscriptForTitles(path)
}

// CustomTitle / FirstQuestion are single-value convenience wrappers: each
// runs the SAME single scan (cache-backed) and discards the other candidate.
// Callers that need both values must use TitleCandidates directly instead of
// calling these two in sequence — the wrapper pair exists for interface
// completeness, not for paired use.
//
// CustomTitle / FirstQuestion 是单值便捷包装：各自跑同一次（带缓存的）扫描，
// 丢弃另一个候选。需要两个值的调用方必须直接用 TitleCandidates，而不是先后
// 调这两个——包装对的存在是为接口完整性，不是为成对使用。
func (claudeTranscriptResolver) CustomTitle(path string) string {
	custom, _ := claudeTranscriptResolver{}.TitleCandidates(path)
	return custom
}

func (claudeTranscriptResolver) FirstQuestion(path string) string {
	_, first := claudeTranscriptResolver{}.TitleCandidates(path)
	return first
}

func (claudeTranscriptResolver) StripRules() []stripRule {
	return claudeNativeStripRules
}

// ─────────────────────────────────────────────────────────────────────────────
// Claude transcript internals — path resolution, content extraction, scanning.
// These are used by claudeTranscriptResolver and by agent.go's display-title
// pipeline. They remain in agent.go; the resolver wraps them.
// ─────────────────────────────────────────────────────────────────────────────

// transcriptTitleCache caches the result of transcript title derivation
// keyed by (sessionID, fileModTime). When the file hasn't changed, the
// cached result is reused instead of re-reading the file. The cache has
// no expiry — entries are invalidated by modTime change — but it is
// bounded by the number of distinct sessions ever listed.
//
// 转录标题派生结果缓存，以 (sessionID, modTime) 为键。文件未变时复用缓存，
// 不再重新读取。缓存无 TTL，通过 modTime 变化淘汰，但大小受限于曾列出过的
// 不同会话数。
var transcriptTitleCache sync.Map // map[string]transcriptTitleResult

type transcriptTitleResult struct {
	CustomTitle   string // newest custom-title record, or ""
	FirstQuestion string // first real user question, or ""
	ModTime       int64  // file.ModTime().UnixNano()
}

// cachedTitleResult returns the cached title result for sid if it exists and
// matches modTime. Extracted from scanTranscriptForTitles to reduce
// cyclomatic complexity.
func cachedTitleResult(sid string, modTime int64) (transcriptTitleResult, bool) {
	cached, ok := transcriptTitleCache.Load(sid)
	if !ok {
		return transcriptTitleResult{}, false
	}
	c, ok := cached.(transcriptTitleResult)
	if !ok || c.ModTime != modTime {
		return transcriptTitleResult{}, false
	}
	return c, true
}

// scanTranscriptForTitles performs a single-pass scan of a transcript file
// extracting both the newest custom-title record and the first real user
// question. This replaces the previous two separate full-file scans, halving
// I/O for large files. The whole file is always scanned (no size cap) so the
// correct title is derived for transcripts of any size; the (sessionID,
// modTime) cache keeps repeated reads of an unchanged file cheap.
//
// 单次扫描转录文件，同时提取最新的 custom-title 记录和第一条真实用户问题。
// 替代之前的两次全文件扫描，将 I/O 减半。始终扫描整个文件（无大小上限），
// 因此任何大小的转录都能得到正确标题；(sessionID, modTime) 缓存使同一未变更
// 文件的重复读取保持廉价。
func scanTranscriptForTitles(path string) (customTitle, firstQuestion string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer func() { _ = f.Close() }()

	// Read modTime for the result cache; the whole file is always scanned so
	// the correct title is derived regardless of size.
	fi, err := f.Stat()
	if err != nil {
		return "", ""
	}
	modTime := fi.ModTime().UnixNano()

	// Check cache.
	sid := filepath.Base(path)
	sid = strings.TrimSuffix(sid, ".jsonl")
	if c, ok := cachedTitleResult(sid, modTime); ok {
		return c.CustomTitle, c.FirstQuestion
	}

	// Read line by line. A bufio.Reader (not a bufio.Scanner) is used so that a
	// single pathologically long line cannot silently abort the scan: Scanner has
	// a hard per-line cap (16MB) beyond which it returns bufio.ErrTooLong and stops,
	// dropping the rest of the transcript (e.g. a tail custom-title or a head
	// first-question). ReadBytes grows its buffer for arbitrarily long lines, so
	// the "always scan the whole file" contract holds for transcripts of any size.
	r := bufio.NewReader(f)
	lastCustom := ""
	foundFirst := false
	for {
		line, readErr := r.ReadBytes('\n')
		if len(line) > 0 {
			var d struct {
				Type        string `json:"type"`
				CustomTitle string `json:"customTitle"`
				Message     *struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal(line, &d); err != nil {
				// Non-JSON / malformed line — skip it and keep scanning.
			} else {
				// Extract custom-title (keep the newest).
				if d.Type == "custom-title" {
					if s := strings.TrimSpace(d.CustomTitle); s != "" {
						lastCustom = s
					}
				} else if !foundFirst && d.Type == strUser && d.Message != nil {
					raw := transcriptContentText(d.Message.Content)
					// Claude transcript first-question extraction strips with the
					// claude rule set (universal + claude-native) — the transcript
					// is always claude's own format.
					text, ok := stripMachineText(raw, stripRulesFor(claudeTranscriptResolver{}))
					if ok {
						if t := strings.TrimSpace(text); t != "" {
							firstQuestion = t
							foundFirst = true
						}
					}
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				// Rare non-EOF read error — log it so the whole-file-scan contract
				// degradation is explicit rather than silent.
				slog.Warn("transcript title scan aborted early",
					slog.String("path", path),
					slog.String("err", readErr.Error()))
			}
			break
		}
	}

	transcriptTitleCache.Store(sid, transcriptTitleResult{
		CustomTitle:   lastCustom,
		FirstQuestion: firstQuestion,
		ModTime:       modTime,
	})
	return lastCustom, firstQuestion
}

// resolveTranscriptPath locates the CLI transcript for a session: first the
// munged project dir for cwd, then a global lookup by session ID (the
// transcript may live under a different project directory). Returns "" when
// no transcript exists.
//
// 定位会话的 CLI 转录文件：先按 cwd 映射的项目目录找（~/.claude/projects/
// <cwd斜杠转横线>/<sid>.jsonl），找不到再按会话 ID 全局兜底（转录可能挂在别的
// 项目目录下）。找不到返回 ""。
func resolveTranscriptPath(home, cwd, sessionID string) string {
	path := acpTranscriptPath(home, cwd, sessionID)
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
		if len(matches) == 0 {
			return ""
		}
		sort.Strings(matches)
		path = matches[0]
	}
	return path
}

// transcriptContentText extracts the plain text from a claude-cli transcript
// user-message "content" field, which claude-code writes in either of two
// shapes:
//   - a plain string: "content": "the question"
//   - a list of blocks: "content": [{"type":"text","text":"..."}, ...]
//
// RawMessage is used so both shapes decode without error; this returns "" for
// any other shape.
//
// 从 claude-cli 转录用户消息的 content 字段提取纯文本。claude-code 的 content
// 有两种形式：纯字符串 "content":"问题"；或块列表 "content":[{type:text,text:...}]。
// 用 RawMessage 解码使两种形式都不报错；其他形式返回 ""。
func transcriptContentText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	// String form: starts with '"'.
	if content[0] == '"' {
		var s string
		if err := json.Unmarshal(content, &s); err == nil {
			return s
		}
		return ""
	}
	// List-of-blocks form.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	raw := ""
	for _, b := range blocks {
		if b.Type == "text" {
			raw += b.Text
		}
	}
	return raw
}

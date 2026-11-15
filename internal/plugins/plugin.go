// Package plugins 是"功能"（feature）的服务端注册表。
//
// 它回答一个问题：这个服务器上有哪些功能、它们现在开没开、哪些 HTTP 路由属于
// 它们、以及它们各自的后台资源怎么起停。
//
// # 为什么只有声明
//
// 这个包**只依赖 internal/model**，与 internal/ai/backends 的位置和约束一致。
// Plugin 里刻意只放"声明"：Start / Stop / Reconfigure / OnDisable 是函数值，
// 由 cmd/server/main.go 在注册时注入。这样本包不需要 import service / handler /
// kb / mail，也就不会与它们成环（service 与 handler 都可能在别的方向上需要
// 读到功能状态）。
//
// # 唯一注册点
//
// 注册只发生在 cmd/server/main.go（registerPlugins），和
// `_ "clawbench/internal/ai/backends/claude"` 那批空白 import 同一个思路：
// main 是唯一知道所有实现的地方，注册表本身不认识任何实现。
//
// # 三种"关闭"不要合并
//
// 这里管的是**功能**（服务端开关）这一轴。能力（capability：环境/宿主本来
// 就没有）与偏好（preference：这台设备只是不显示入口）各有各的位置，不能
// 因为"统一"而并成一个布尔。Available 描述的是能力轴，它与 Enabled 是两件事：
// 终端可以 enabled=true 而 available=false（Windows 没有 PTY）。
package plugins

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"clawbench/internal/model"
)

// Plugin 是一个功能的完整自描述单元。
type Plugin struct {
	// ID 是稳定标识，同时是前端页签 id、/api/features 的键、i18n 错误键
	// 的前缀。全小写，不允许改（改了等于换了一个功能）。
	ID string

	// Section 是 config.yaml 里的段名（"mail" / "forge" / "kb" / …）。
	// 空表示这个功能没有服务端总开关（纯偏好型，如"统计"）。
	//
	// 它派生两件事：PATCH 白名单的守卫范围（该段下每个 yaml 字段都必须在
	// PatchableConfigPaths 里）与文档里"这个功能由哪个配置段控制"。
	// 白名单本身仍手写——反射派生白名单等于把安全边界交给反射。
	Section string

	// Enabled 读取当前是否开启。nil 表示恒开。
	//
	// 必须读实时值（model.ConfigInstance / model.TasksEnabled 这类包级
	// 全局），不能读启动时的快照：热更新会在进程生命周期内改它。
	Enabled func() bool

	// DefaultOn 只用于文档与"缺键当开"的语义声明；真正的默认值由
	// model.ApplyDefaults 的 presence 检查决定（那里才是唯一实现）。
	DefaultOn bool

	// APIPrefixes 声明"这些路由属于我"。注册路由时命中的前缀会被自动套上
	// 功能门控（见 gate.go），因此新增一个接口只要落在前缀下就自动被拦，
	// 不需要任何人记得写检查。
	//
	// 写法：精确路径（"/api/mail/send"）或子树前缀（"/api/mail/"）都行，
	// 匹配规则见 ForRoute。有 Section 的插件必须声明它（守卫测试会拒绝空值）。
	APIPrefixes []string

	// Start / Stop / Reconfigure 是后台资源的生命周期。
	//
	// Start 必须幂等（重复调用不叠加 goroutine），Stop 必须幂等。
	// Reconfigure 在热更新后调用，用于"改了间隔 / 改了批次大小"这类不需要
	// 重启的调整。三者都可为 nil。
	Start       func(cfg model.Config)
	Stop        func()
	Reconfigure func(cfg model.Config)

	// OnDisable 在开关从 true 变 false **且配置已成功落盘之后**调用，
	// 用于"关闭即撤销"这类不可逆动作（清空仓库绑定、撤销分享链接、
	// 取消运行中的任务）。返回 warnings（部分失败时报告给客户端，但不回滚
	// 配置）。
	//
	// 时机不能改：先撤销再写盘失败会毁掉用户的数据，而开关其实还开着。
	OnDisable func() []string

	// DisabledMsgKey 是接口被拒时的 i18n 键（如 "ForgeDisabled"）。
	// 空则按 DisabledKey 的约定从 ID 推导。
	DisabledMsgKey string

	// DisabledStatus 是拒绝时的 HTTP 状态码。0 表示 409 Conflict
	// （"你有权限，功能关着"，客户端统一处理）。终端用它报 503，
	// 因为那是"运行时没起来"而不是"被关掉了"。
	DisabledStatus int

	// Available 是**运行时**可用性（不是配置）：宿主跑不跑得起来。
	// nil 表示恒可用。目前只有终端会 false（PTY 没起来 / 平台不支持）。
	//
	// 与 Enabled 是两条轴：enabled=true + available=false 表示"功能开着，
	// 但这台服务器跑不了"，前端要灰显并给出原因，而不是隐藏。
	Available func() bool

	// GateExempt 列出"功能关着也必须能访问"的路径（如 Forge 的
	// GET /api/forge/binding：前端靠它判断"未绑定"，关闭时它要返回空而
	// 不是 409）。豁免必须显式声明，不能靠漏配前缀。
	//
	// 写法："路径" 表示所有方法豁免；"METHOD 路径" 只豁免该方法
	// （Forge 的 binding 就是这样：GET 放行、POST/DELETE 仍被拒）。
	GateExempt []string
}

// DisabledKey 返回拒绝请求时使用的 i18n 键。
//
// 约定：<ID 首字母大写>Disabled，例如 "forge" → "ForgeDisabled"。
func (p *Plugin) DisabledKey() string {
	if p.DisabledMsgKey != "" {
		return p.DisabledMsgKey
	}
	if p.ID == "" {
		return ""
	}
	return strings.ToUpper(p.ID[:1]) + p.ID[1:] + "Disabled"
}

// DisabledHTTPStatus 返回拒绝请求时使用的状态码，0 归一为 409。
func (p *Plugin) DisabledHTTPStatus() int {
	if p.DisabledStatus == 0 {
		return 409
	}
	return p.DisabledStatus
}

// IsEnabled 报告功能是否开启（Enabled 为 nil 视为恒开）。
func (p *Plugin) IsEnabled() bool {
	return p.Enabled == nil || p.Enabled()
}

// IsAvailable 报告运行时是否可用（Available 为 nil 视为恒可用）。
func (p *Plugin) IsAvailable() bool {
	return p.Available == nil || p.Available()
}

// IsGateExempt 报告 method + path 是否落在豁免清单里。
//
// 这里比 ForRoute 严格，因为"豁免"是在开一个洞：
//   - 尾部带 "/" 的条目是子树，只豁免它**下面**的路径（"/api/tasks/" 不会
//     顺手放过 "/api/tasks" —— 那正是"新建任务"要拦的那条）；
//   - 不带 "/" 的条目是精确路径，不做前缀匹配（"/api/forge/binding" 不会
//     放过 "/api/forge/binding-extra"）。
//
// 带方法前缀的条目只豁免该方法。
func (p *Plugin) IsGateExempt(method, path string) bool {
	for _, entry := range p.GateExempt {
		exemptMethod, target := splitMethodSpec(entry)
		if exemptMethod != "" && !strings.EqualFold(exemptMethod, method) {
			continue
		}
		if exemptPathMatches(path, target) {
			return true
		}
	}
	return false
}

// exemptPathMatches 按 IsGateExempt 的严格语义匹配。
func exemptPathMatches(path, target string) bool {
	if strings.HasSuffix(target, "/") {
		// Subtree: strictly below the prefix.
		return strings.HasPrefix(path, target)
	}
	return trimPath(path) == target
}

// splitMethodSpec 拆开可选的 "METHOD 路径" 写法。
func splitMethodSpec(entry string) (method, path string) {
	if i := strings.IndexByte(entry, ' '); i > 0 {
		return entry[:i], strings.TrimSpace(entry[i+1:])
	}
	return "", strings.TrimSpace(entry)
}

var (
	mu      sync.RWMutex
	byID    = map[string]*Plugin{}
	ordered []*Plugin // 按 ID 升序，Register 时维护
)

// Register 把一个功能登记进注册表。
//
// 重复 ID、空 ID、非法前缀都是编程错误，直接 panic：静默覆盖会让两个功能
// 抢同一批路由，而那是排查起来最费劲的一类 bug。
func Register(p *Plugin) {
	if p == nil {
		panic("plugins: Register(nil)")
	}
	if p.ID == "" {
		panic("plugins: Register with empty ID")
	}
	for _, prefix := range p.APIPrefixes {
		if !strings.HasPrefix(prefix, "/") {
			panic(fmt.Sprintf("plugins: %s declares APIPrefix %q, which must start with /", p.ID, prefix))
		}
	}
	for _, exempt := range p.GateExempt {
		if _, target := splitMethodSpec(exempt); target == "" {
			panic(fmt.Sprintf("plugins: %s declares an empty GateExempt entry", p.ID))
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if _, dup := byID[p.ID]; dup {
		panic(fmt.Sprintf("plugins: duplicate plugin ID %q", p.ID))
	}
	byID[p.ID] = p
	// 保持 ordered 按 ID 升序：/api/features 与 worker 启停顺序都要稳定，
	// 不能依赖注册调用的先后。
	ordered = append(ordered, p)
	slices.SortFunc(ordered, func(a, b *Plugin) int { return strings.Compare(a.ID, b.ID) })
}

// Lookup 按 ID 查找插件，未注册返回 nil。
func Lookup(id string) *Plugin {
	mu.RLock()
	defer mu.RUnlock()
	return byID[id]
}

// All 返回全部插件（按 ID 升序的副本）。
func All() []*Plugin {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Plugin, len(ordered))
	copy(out, ordered)
	return out
}

// ForRoute 返回声明了 path 所属前缀的插件，没有则 nil。
//
// 规则（两条都要，缺一条就会出错）：
//
//   - **最长前缀优先**："/api/git/stats" 的声明要压过 "/api/git/"。
//   - **按路径段边界匹配**：前缀 "/api/mail" 不能匹配 "/api/mailbox"。
//     实现上就是"路径相等，或路径以 前缀+"/" 开头"，因此两端尾部的 "/"
//     先被抹平——路由表里 "/api/tasks" 与 "/api/tasks/" 是两个 pattern，
//     而插件只声明一次，两种写法都必须命中。
func ForRoute(path string) *Plugin {
	clean := trimPath(path)
	mu.RLock()
	defer mu.RUnlock()
	var best *Plugin
	bestLen := -1
	for _, p := range ordered {
		for _, prefix := range p.APIPrefixes {
			if !pathMatches(clean, prefix) {
				continue
			}
			// 用规范化后的长度比较，避免 "/api/x/" 与 "/api/x" 互相打架。
			if n := len(trimPath(prefix)); n > bestLen {
				best, bestLen = p, n
			}
		}
	}
	return best
}

// Reset 清空注册表。**仅供测试**：生产代码里注册表一旦建立就不该再变，
// 否则已经套上 Gate 的路由与 /api/features 会互相矛盾。
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	byID = map[string]*Plugin{}
	ordered = nil
}

// trimPath 抹平尾部斜杠，让精确路径与子树前缀共用一套比较。
func trimPath(path string) string {
	if path == "/" {
		return path
	}
	return strings.TrimSuffix(path, "/")
}

// pathMatches 报告已规范化的路径是否落在 prefix 上（精确或按段边界）。
func pathMatches(cleanPath, prefix string) bool {
	norm := trimPath(prefix)
	if norm == "" || norm == "/" {
		return false
	}
	return cleanPath == norm || strings.HasPrefix(cleanPath, norm+"/")
}

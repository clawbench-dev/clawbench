// Package config 收拢"配置属于哪个功能"这件事。
//
// 它回答三个问题，且三者的答案都写在同一处（每个功能一个文件）：
//
//   - 这个功能允许 PATCH 哪些配置路径（Fields）；
//   - 缺省时这些键应该是什么值（后续迁入 Defaults）；
//   - 一次 PATCH 里哪些值非法、怎么写进 model.Config（后续迁入 Validate/Apply）。
//
// 为什么单独成包，而不是放进 internal/plugin/<id>：
//
//	handler 必须在**插件注册表为空**时也能回答"这个路径能不能改"——它的
//	单测从不调用 registerPlugins（否则每个配置测试都要拉起整条服务依赖）。
//	所以这份知识不能挂在运行时注册表上，只能是一个可被 handler 直接 import
//	的静态包。本包刻意不 import 任何插件实现，避免把 service / ai 拖进
//	handler 的测试依赖。
//
// 与 internal/model 的关系：本包 import model（配置结构体在那里），model
// 不 import 本包。因此 model.ApplyDefaults 仍然只负责"宿主自己的字段"，
// 功能的默认值由 main 在配置加载后调用本包补上。
package config

import "clawbench/internal/model"

// Feature 是一个功能的配置声明。
//
// 目前只有 Fields；Defaults / Validate / Apply 会随着迁移陆续加进来，届时
// 加配置的改动面从"model + handler 三处"缩到"本包一个文件"。
type Feature struct {
	// ID 必须等于 plugins.Plugin.ID 与 config.yaml 的段名。
	ID string

	// Fields 是这个功能允许被 PATCH /api/config 修改的配置路径。
	//
	// 键是完整路径（"mail.sync_interval_minutes"）。值是 true 而不是说明
	// 文字：说明属于文档，写进字符串会被 i18n 守卫当成"未翻译的用户可见
	// 文案"拦下。
	Fields map[string]bool

	// Defaults 在这个功能负责的键缺省时填入默认值。
	//
	// present 回答"这个键在 config.yaml 里出现过吗"——必须用它而不是零值
	// 判断：Go 的 bool 零值是 false，直接判断会把"用户没写"读成"用户关了"，
	// 升级一次就静默关掉所有功能。
	Defaults func(cfg *model.Config, present func(string) bool)

	// Validate 在一次 PATCH 落盘前检查本功能段里的值。nil 表示没有额外校验
	// （类型已在解析阶段保证）。
	Validate func(patch map[string]any) error

	// Apply 把本功能段写进 cfg。校验已经跑过；这里只做赋值，以及那些
	// 必须与配置同时更新的包级全局（tasks.enabled、share 权限）。
	Apply func(cfg *model.Config, patch map[string]any)
}

// all 是全部功能的配置声明。
//
// 加一个带配置的功能：新建 <id>.go 并在下面加一行。漏加的表现是它的字段
// 全部不可 PATCH（设置页保存被 400 拒绝），不会静默写坏配置。
var all = []*Feature{
	Forge,
	KB,
	Mail,
	Share,
	Tasks,
	Terminal,
}

// All 返回全部功能的配置声明，顺序与 all 一致。
func All() []*Feature {
	return all
}

// ByID 按功能 id 取声明；不存在时返回 nil。
func ByID(id string) *Feature {
	for _, f := range all {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// Fields 返回某个功能的可改字段清单，供 plugins.Plugin.ConfigFields 引用。
//
// 调用方只读；返回 nil 表示这个功能没有配置（不是空 map，避免被误认为
// "声明了零个字段"）。
func Fields(id string) map[string]bool {
	if f := ByID(id); f != nil {
		return f.Fields
	}
	return nil
}

// Owns 报告 path 是否由某个功能声明。
//
// handler 的 PATCH 白名单用它，因此它必须能在注册表为空时工作——这正是
// 本包存在的原因。
func Owns(path string) bool {
	for _, f := range all {
		if f.Fields[path] {
			return true
		}
	}
	return false
}

// AllFields 返回全部功能字段的并集。
//
// 供守卫测试做"某个对象下面有没有可改字段"这类前缀判断——只查宿主字段会
// 把 forge.notify 这类中间层判成孤儿。
func AllFields() map[string]bool {
	out := make(map[string]bool)
	for _, f := range all {
		for path := range f.Fields {
			out[path] = true
		}
	}
	return out
}

// Validate 依次跑每个功能的校验，遇到第一个错误就返回。
//
// 顺序与 all 一致，所以"同一个 PATCH 里两处都非法"时报哪一条是确定的。
func Validate(patch map[string]any) error {
	for _, f := range all {
		if f.Validate == nil {
			continue
		}
		if err := f.Validate(patch); err != nil {
			return err
		}
	}
	return nil
}

// Apply 把每个功能段写进 cfg。
//
// 与 Validate 分开：handler 先校验、再写盘、最后调用本函数，这样一次失败的
// PATCH 不会留下半套已应用的值。
func Apply(cfg *model.Config, patch map[string]any) {
	for _, f := range all {
		if f.Apply != nil {
			f.Apply(cfg, patch)
		}
	}
}

// ApplyDefaults 让每个功能补齐自己那一段的缺省值。
//
// 调用点在 main：model.ApplyDefaults 先处理宿主字段（端口、语言、上传…），
// 本函数随后处理功能字段。之所以不在 model 里做，是因为 model 不能 import
// 本包（会成环），而本包必须能被 handler 直接 import（它的单测不注册插件）。
func ApplyDefaults(cfg *model.Config, presence map[string]bool) {
	present := func(key string) bool { return presence[key] }
	for _, f := range all {
		if f.Defaults != nil {
			f.Defaults(cfg, present)
		}
	}
}

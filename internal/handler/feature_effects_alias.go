package handler

import (
	"clawbench/internal/plugin/forge"
	"clawbench/internal/plugin/share"
)

// 关闭功能的清理钩子住在各自的插件包里，但 handler 的开关测试需要直接调用
// 它们验证"清理失败只报 warning、不返回 500"——这条契约由 handler 的
// PATCH /api/config 流程保证（applyPluginDisableEffects），所以测试留在
// 这一侧。
//
// 依赖方向是 handler → plugin/<id>，不成环（插件包不 import handler）。
var (
	ForgeDisableEffect = forge.ForgeDisableEffect
	ShareDisableEffect = share.ShareDisableEffect
)

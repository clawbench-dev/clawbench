package handler

import "clawbench/internal/cfgwrite"

// 配置写入已经从 handler 搬到 internal/cfgwrite，因为 forge 功能（写凭据、
// 绑定仓库）需要它，而功能搬进 internal/plugin/<id> 之后不能再 import handler。
//
// 保留同名别名，是为了不动本包内十几处调用点。锁是同一把：settings.go 里的
// configMutex 现在指向 cfgwrite.Mu。
var writeConfigYAML = cfgwrite.Patch

// 测试里用它构造配置 map。
var mergePatchIntoRaw = cfgwrite.MergePatchIntoRaw

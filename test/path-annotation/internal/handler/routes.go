package handler

// 相对路径引用同级目录文件: test/path-annotation/internal/handler/routes.go
// See routes.go for endpoint definitions
// See config/settings.json for configuration
// See ../../README.md for project overview

// 项目外路径: /etc/hosts
var hostsRef = "/etc/hosts"

// 纯扩展名文件名: go.mod（应以文件目录为基准解析）
var modFile = "go.mod"

// 不存在的相对路径，应 fallback 到项目根
var fallback = "go.mod"

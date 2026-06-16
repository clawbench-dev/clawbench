# Path Annotation Test

This directory contains test fixtures for verifying file path annotation behavior
in the web frontend. These files are **not** meant to be compiled or executed.

- Go files have `//go:build exclude` tags to prevent compilation
- The vitest config excludes `test/path-annotation/**` from test discovery

## Markdown 标注测试

### 项目内路径（蓝色）

- `web/src/App.vue`
- `README.md`
- `go.mod`
- `./go.mod`
- `../README.md`

### 项目外路径（橙色）

- `/etc/hosts`
- `/home/xulongzhe/.bashrc`
- `~/.bashrc`
- `/var/log/syslog`

### 相对路径（以文件所在目录为基准）

本文件在 `test/path-annotation/` 目录下，所以：

- `README.md` → `test/path-annotation/README.md`
- `internal/config/settings.json` → 相对当前目录
- `../README.md` → 项目根 README.md

### 不应标注

- fmt
- net/http
- https://example.com
- $HOME/.bashrc
- src/**/*.go

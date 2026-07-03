[中文](CONTRIBUTING.md) | [English](CONTRIBUTING.en.md)

# 贡献指南

感谢你对 ClawBench 的关注！欢迎通过以下方式参与贡献：

- 🐛 [报告 Bug](../../issues/new?template=bug_report.yml)
- 💡 [建议新功能](../../issues/new?template=feature_request.yml)
- ❓ [提出问题](../../issues/new?template=question.yml)
- 🔧 提交代码或文档

## 行为准则

- 保持尊重和建设性
- 优先使用中文交流，英文也欢迎
- 中英文之间加空格，提升可读性

---

## 如何贡献

### 报告 Bug

1. 使用 [Bug 报告模板](../../issues/new?template=bug_report.yml)
2. 提供清晰的复现步骤、期望行为、实际行为
3. 附上环境信息（版本号、操作系统、浏览器/Android 版本）
4. 如有可能，附上日志或截图

### 建议 Feature

1. 使用 [Feature 请求模板](../../issues/new?template=feature_request.yml)
2. 描述你遇到的问题场景，而非仅描述解决方案
3. 说明考虑过的替代方案

### 提问

使用 [问题咨询模板](../../issues/new?template=question.yml) 或 [GitHub Discussions](../../discussions)。

---

## 开发流程

### 环境准备

- Go 1.25+
- Node.js 22+
- JDK 17（Android 开发）

```bash
git clone https://github.com/xulongzhe/clawbench.git
cd clawbench
```

构建与运行命令详见 [AGENTS.md](AGENTS.md)。

### 分支策略

| 分支 | 用途 |
|------|------|
| `main` | 稳定分支，仅通过 PR 合入 |
| `feat/<描述>` | 新功能开发 |
| `fix/<描述>` | Bug 修复 |
| `docs/<描述>` | 文档更新 |

### 开发 → 提交 → PR

1. 基于 `main` 创建分支：`git checkout -b feat/your-feature`
2. 开发并提交（遵循下方 Commit 规范）
3. 推送并创建 PR
4. CI 通过后等待合并

---

## Commit 规范

### 格式

```
<type>(<scope>): <描述>
```

- **type**：英文，必填
- **scope**：英文模块名，可选
- **描述**：中文优先，中英文之间加空格，中文不加句号

### Type 列表

| Type | 用途 |
|------|------|
| `feat` | 新功能 |
| `fix` | Bug 修复 |
| `docs` | 文档变更 |
| `style` | 代码格式（不影响逻辑） |
| `refactor` | 重构（非新功能/修复） |
| `perf` | 性能优化 |
| `test` | 测试用例 |
| `chore` | 构建/工具/依赖 |
| `ci` | CI 配置 |
| `build` | 编译产物/构建脚本 |
| `revert` | 回滚提交 |

### Scope 参考

`android`、`rag`、`task`、`scheduler`、`tts`/`speech`、`ssh`、`ws`、`terminal`、`config`、`ci`

### 示例

```
feat(android): 推送通知显示 AI 回复预览
docs: 补充 RAG 部署文档
refactor(scheduler): 优化定时任务调度逻辑
test: 改善后端测试覆盖 — internal/handler
chore: 升级 Go 版本至 1.25
```

---

## PR 规范

### 标题格式

与 commit message 统一：

```
<type>(<scope>): <描述>
```

### 描述模板

PR 创建时会自动加载模板，包含以下字段：

- **变更说明**：做了什么、为什么做
- **变更类型**：勾选对应类型
- **关联 Issue**：`Fixes #N` / `Closes #N`
- **测试**：已通过的测试和验证方式
- **自检清单**：代码风格、敏感信息、文档

### 流程规则

- 必须关联 Issue
- CI 必须通过（Go / Frontend / Android 覆盖率 gate）
- 使用 Squash Merge 合入 main

---

## 代码风格

- **Go**：`gofumpt`（通过 golangci-lint）+ `golangci-lint v2` 全量严格
- **Vue / TypeScript**：遵循项目已有配置
- 变更须有测试覆盖

## 测试

```bash
go test ./...        # Go 全量测试
npm test             # 前端全量测试
```

CI 强制执行覆盖率 gate：
- **Tier 1**：每包/目录覆盖率不低于基线 -1.5%
- **Tier 2**：变更行覆盖率不低于 80%

详见 [AGENTS.md](AGENTS.md) 中的"Coverage gate"章节。

## Lint

```bash
./scripts/lint-go.sh              # Go 全量 lint
./scripts/lint-go.sh --fix        # 自动修复可修复的问题
./scripts/lint-go.sh --diff       # 仅检查暂存区变更
```

### nolint 使用规范

仅在真正合理的场景使用 `//nolint`，且必须附带原因说明：

```go
//nolint:errcheck // 有意忽略：只读响应的 Close() 错误
resp.Body.Close()
```

禁止：
- 不带原因的 `//nolint`
- 为绕过问题批量添加 `//nolint`
- 降低 linter 阈值来绕过问题

## 发布流程

- Git tag 驱动，`release.yml` 自动构建多平台产物
- Squash merge 到 main → 打 tag → 自动发布

## 许可证

本项目基于 [MIT License](LICENSE) 开源。提交贡献即表示你同意代码以 MIT 许可证发布。

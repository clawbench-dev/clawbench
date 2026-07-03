[中文](FAQ.md) | [English](FAQ.en.md)

# 常见问题（FAQ）

**Q: ClawBench 支持哪些操作系统？**

A: 支持 Linux（x86_64 / ARM64）和 Windows（x86_64）。后端使用 Go 编写，前端为标准 Web 应用，可跨平台运行。

**Q: 支持哪些 AI 后端？**

A: 支持 CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi 九种 CLI 后端，以及 Gemini/Kimi（ACP 模式）。可在 Web UI 中实时切换，会话数据隔离。CLI 后端需确保对应 CLI 已安装并在 PATH 中可用。

**Q: 如何添加新的智能体？**

A: 从欢迎屏幕安装智能体，或在 Web UI 中创建新智能体，选择 LLM 提供商、输入 API Key、验证模型、命名智能体即可。智能体配置存储在数据库中。公共规则内嵌于 Go 二进制（`commonRulesTemplate`），会自动注入到所有智能体的系统提示词中。`{{AVAILABLE_AGENTS}}` 占位符会自动替换为可用智能体列表。`@chatsearch`/`@task` 命令按需注入。

**Q: 是否需要配置 API Key？**

A: 不需要。ClawBench 通过调用本地 CLI（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code 或 Pi）实现 AI 功能，这些 CLI 工具已经完成了 API Key 的配置和管理。

**Q: TTS 语音合成可以使用本地模型吗？**

A: 可以。将 `summarize.backend` 设为 `"api"` 并配置 Ollama 的 OpenAI 兼容端点即可使用本地 Ollama 服务进行文本总结，无需任何云 API。只需安装 Ollama 并拉取模型（如 `ollama pull gemma3:270m`），然后在配置文件中设置：

```yaml
summarize:
  backend: "api"
  model: "gemma3:270m"
  api:
    base_url: "http://localhost:11434/v1/chat/completions"
    format: "openai"
```

TTS 引擎本身也支持本地离线方案（piper / kokoro / moss-nano），两者搭配可实现完全离线的语音朗读。其中 moss-nano 支持多语言和音色克隆，48kHz 高音质输出。

**Q: 可以同时运行多个 ClawBench 实例吗？**

A: 可以。将整个发布目录复制到不同位置，每个副本拥有独立的 `BinDir`、配置和 `.clawbench/` 数据目录，完全隔离。只需在各副本的 `config/config.yaml` 中配置不同端口即可。

**Q: 是否需要配置文件才能启动？**

A: 不需要。所有配置项均有默认值，无需 `config/config.yaml` 即可启动。未配置 `password` 时自动生成随机密码并保存到 `.clawbench/auto-password`，启动时会自动显示。如需自定义，复制 `config/config.example.yaml` 为 `config/config.yaml` 并修改。

**Q: 忘记自动生成的密码怎么办？**

A: 查看 `.clawbench/auto-password` 文件即可获取密码。也可以在 `config/config.yaml` 中设置 `password` 来使用固定密码。

**Q: 数据存储在哪里？**

A: 数据存储在二进制文件同级目录下的 `.clawbench/` 中，包括数据库文件（`ClawBench.db`）、日志文件（`logs/`）和自动密码（`auto-password`）。上传的文件存放在项目目录的 `.clawbench/uploads/` 中。绿色便携，删除程序目录即可彻底卸载。

**Q: 如何备份数据？**

A: 备份二进制同级目录下 `.clawbench/ClawBench.db` 数据库文件即可。

**Q: 如何管理智能体？**

A: 所有智能体存储在数据库中（`agents` 表），通过设置向导创建或首次启动时自动发现。API Key 加密存储在 `agent_api_keys` 表（AES-256-GCM），由系统自动管理。

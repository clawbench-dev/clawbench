# 插件开发指南

给"要给 ClawBench 加一个功能"的人看的手册。读完应该能照着做，不用再翻代码找第二处要改的地方。

> **"插件"在这个项目里指内部功能模块**：随应用一起编译发布，不支持远程加载代码，也没有沙箱。目标只有一个——**加一个功能的边际成本接近零，删一个功能的代价是删掉一个目录。**

---

## 1. 先分清两个"插件"

前端和后端各有一套，**互相独立**，靠 `id` 对上：

| | 界面插件（Dock） | 功能插件（服务端） |
|---|---|---|
| 目录 | `web/src/plugins/<id>/` | `internal/plugin/<id>/` |
| 回答的问题 | 左边栏有没有这个页签、点开显示什么 | 这个服务器收不收这些请求、后台跑不跑东西 |
| 谁发现它 | `import.meta.glob('@/plugins/*/plugin.ts')`，**建目录即被发现** | `internal/plugin/all/all.go` 里一行 `xxx.Register()` |
| 加一个要改哪 | 只建目录 | 建目录 + `all.go` 加一行 |
| 能不能单独存在 | 能（纯界面页签，如文件浏览、设置） | 能（没有页签的功能，如统计） |

三条最短路径：

- **只加一个界面页签** → 只建 `web/src/plugins/<id>/`，后端一行不动。
- **只加一个后端能力**（没有页签，或页签已存在）→ 建 `internal/plugin/<id>/` + `all.go` 一行。
- **加一个带开关的完整功能**（最常见）→ 两边都建，`id` 相同，前端 `feature` 指向后端那个开关。见 [03-contract.md](03-contract.md)。

---

## 2. id 的规矩

`id` 是整个系统的连接键，同时是：

- 前端页签 id、`web/src/plugins/<id>/` 的目录名；
- 后端插件 `Plugin.ID`、`/api/features` 里的键；
- 服务端 `config.yaml` 的段名（有总开关时，`Section` 通常等于 id）；
- i18n 错误键前缀（`mail` → `MailDisabled`）。

规矩：**全小写、短、稳定、英文**。改了等于换了一个功能——用户存着的页签顺序、服务端配置、分享链接里的引用都会对不上。

少数历史例外：目录名和设置分类名不一致（`browse`→`files`、`live`→`meeting`、`proxy`→`portForward`、`forge`→`forgeIntegration`、`view`→`fileView`）。这类映射写在 `settingsFieldMap.ts` 的一张表里，新增插件不要制造新的例外。

---

## 3. 现在有哪些

界面插件 15 个，其中 9 个有对应的后端功能插件：

| id | 界面 | 后端 | 服务端开关 |
|---|---|---|---|
| `browse` | 文件管理 | — | — |
| `view` | 文件浏览 | — | — |
| `settings` | 设置 | — | — |
| `chat` | 右侧对话区（不是左栏页签） | — | — |
| `history` | 历史会话 | — | — |
| `proxy` | 端口映射 | — | — |
| `forge` | GitHub / GitLab | ✅ | `forge.enabled` |
| `terminal` | 终端 | ✅ | `terminal.enabled` |
| `mail` | 邮件 | ✅ | `mail.enabled` |
| `kb` | 知识库 | ✅ | `kb.enabled` |
| `tasks` | 定时任务 | ✅ | `tasks.enabled` |
| `share` | 分享中心 | ✅ | `share.enabled` |
| `live` | 会议纪要 | ✅ | —（恒开） |
| `notifications` | 通知中心 | ✅ | —（恒开） |
| `stats` | 数据统计 | ✅ | —（恒开） |

`live` / `notifications` / `stats` 注册后端插件但**没有总开关**：它们的目的是让 `/api/features` 能回答"这个功能存在"，前端不必按页签 id 硬编码清单。

---

## 4. 文档索引

| 文件 | 讲什么 |
|---|---|
| [01-frontend.md](01-frontend.md) | 界面插件：`plugin.ts` 每个字段、`i18n.ts`、`settings.ts`、面板契约、三个轴 |
| [02-backend.md](02-backend.md) | 功能插件：`Plugin` 每个字段、路由挂载、门控、启停、配置字段 |
| [03-contract.md](03-contract.md) | 两边怎么对上：`id`、开关、`/api/features`、错误键；加功能的完整清单 |
| [04-testing.md](04-testing.md) | 前后端各有哪些守卫、覆盖率怎么算、夹具怎么用 |
| [05-pitfalls.md](05-pitfalls.md) | 踩过的坑，按"加插件时最容易忘"排序 |
| [../spec/features/plugin-registry.md](../spec/features/plugin-registry.md) | 后端注册表**为什么这么设计**、哪些不变量被测试钉住 |

---

## 5. 一句话原则

**一个功能的全部事实应该住在它自己的目录里**：叫什么、排第几、有哪些设置项、哪些接口、开关关掉做什么、后台跑什么。

唯一的例外是配置声明（`internal/plugin/config/<id>.go`）：它必须能被 handler 直接 import，而 handler 的单测从不注册插件，所以不能挂在 `internal/plugin/<id>` 的运行时注册表上。理由见 [02-backend.md](02-backend.md) §6。

凡是"加功能时要记得去另一个文件补一行"的地方，都是这个项目还没做完的地方——遇到了要么补掉，要么在 [05-pitfalls.md](05-pitfalls.md) 里写清楚为什么必须这样。

# 技能加载模式加固（P0 → P2）

> **状态：已实施（2026-11-15）**。八步全部落地，行为变更见文末「发布注意」。

> 2026-11-15。针对现有 `internal/skill` 的加载与注入链路做一轮加固。
> 现状评估见文末「问题清单」，本文只写「怎么改」。

## 一、目标

1. 收掉唯一的安全面：打开陌生仓库时，仓库里的技能能往系统提示里塞内容。
2. 让「装了就能用、删了就没」成立，不再依赖手动点重扫。
3. 补上可解释性：为什么某个技能没出现、同名到底哪个生效。
4. 为后续接 SkillHub（市场安装）留好溯源位置。

## 二、非目标

- 不做技能市场本身（另开一份方案）。
- 不引入文件监听（inotify/FSEvents）——跨平台成本高，本轮用目录戳校验替代。
- 不改变 `/` 与 `#` 两个入口的语义（见决策 4）。
- 不做技能的沙箱执行（技能本来就是提示词，没有可执行的容器概念）。

## 三、关键决策

**决策 1：项目技能默认不自动注入。**
新增 `skills.project_auto_inject`，默认 `false`。理由是项目技能来自工作目录，可能是一个刚 clone 的陌生仓库；它的 `description` 会原文进入系统提示，且每轮都在。列表照常展示，`#` 引用照常可用，只是不再自动塞进提示。

**决策 2：优先级改为 `原生 > 用户目录 > 项目 > 仓库 > 其他智能体`。**
即把 `SourceProject` 与 `SourceUserDir` 的数值对调。理由同上：用户自己写的技能不该被工作目录里的同名文件顶掉，`#name` 也走同一套排序，必须一起改。

**决策 3：单技能开关的语义是「不自动注入」，不是「禁用」。**
与 `Skills.Enabled` 的既有约定一致——显式 `#` 引用是用户的直接指令，永远不受开关影响。UI 文案必须按这个语义写，否则用户会以为关了就调不动。

**决策 4：`/` 保持给智能体自己的命令，`#` 是 ClawBench 唯一入口。**
不统一是因为 `/` 由各后端自己解析：ClawBench 把 `/foo` 原样发给 agent，agent 只会去自己的技能目录找。要让 `/` 支持任意来源，就得在发送前拦截并改写成注入，和 agent 自己的命令命名空间直接冲突（`/compact` 这类）。收益不值这个风险，本轮只在文档和提示里讲清分工。

**决策 5：刷新用「目录戳」而不是监听。**
每次注入/列表前，对每个来源根目录取 `(mtime, 直接子项数)`，与上次扫描时比对；变了就重扫。带 2 秒节流，避免每轮都读目录。

## 四、分步实施

每一步独立可交付、可测试、可回滚。

### 步骤 1（P0）项目技能默认不自动注入

| 位置 | 改动 |
|---|---|
| `internal/model/skills.go` | `SkillsConfig` 增加 `ProjectAutoInject bool`（`yaml:"project_auto_inject"`），零值即 false，无需迁移 |
| `internal/skill/registry.go` | `InjectedForProject` 只在开关打开时 `append(r.scanProject(projectPath)...)` |
| `internal/handler/settings.go` | 两处允许键表（约 259、1260 行）加 `skills.project_auto_inject`；PATCH 分支读布尔并计入 `skillsChanged`（要重扫/失效缓存） |
| `internal/handler/skills.go` | `GET /api/skills` 响应加 `project_auto_inject`，UI 才能显示开关状态 |
| `docs/user-guide/configuration.md` | 加一行说明（`config_reference_test.go` 会拦缺行） |

测试：
- 开关关闭时，项目技能不在注入表里，但仍在 `AllForProject` 列表里、`#name` 仍能解析。
- 开关打开时行为与现在一致。
- PATCH 后缓存失效（改完立刻生效，不用重启）。

注意：这是行为变更，发布说明要写清「项目技能需要手动打开自动注入」。默认值不要为了兼容写成 true，否则这一步等于没做。

### 步骤 2（P0）优先级对调

| 位置 | 改动 |
|---|---|
| `internal/skill/types.go` | `SourceProject = 2`、`SourceUserDir = 1`，同步改注释里「项目最具体所以最高」的论述 |
| `web/src/composables/useSkillsState.ts` | `SOURCE_KIND_PRIORITY` 改为 `own, user, project, git, other` |

测试：
- `registry_test.go` 里断言「项目压过用户」的用例要改成反向（这些断言是旧决策的固化，不是契约）。
- `skillSourcePriorityParity.test.ts` 从 Go 源码解析顺序，会自动跟随；必须确认它是因为 TS 侧也改了才通过，而不是解析失败后放行。

### 步骤 3（P0）注入表里的描述截断

| 位置 | 改动 |
|---|---|
| `internal/skill/scanner.go` | `BuildSystemPromptSection` 对 `Description` 按 rune 截断到 200，超出加 `…`；API 列表仍返回完整描述 |

测试：超长描述被截断且表格结构完好；中文按字截断不切坏多字节。

### 步骤 4（P1）单技能「不自动注入」

| 位置 | 改动 |
|---|---|
| `internal/model/skills.go` | `SkillsConfig` 增加 `Disabled []string`（`yaml:"disabled"`），存规范化后的技能名 |
| `internal/skill/registry.go` | `dedupe` 在按名字分组后、选优之前过滤掉禁用名（同名全部来源一起关掉） |
| `internal/handler/settings.go` | 允许键 + 校验（非空、去重、上限 500 条），计入 `skillsChanged` |
| `internal/handler/skills.go` | 响应加 `disabled` 与每行 `injected: bool` |
| `web/src/components/settings/SkillsDiscoveredSetting.vue` | 每行一个开关，文案「自动注入」 |
| `web/src/composables/useSkillsState.ts` | `setSkillInjected(name, on)`，成功后重载 |

测试：关掉后注入表里没有、列表里还有、`#name` 仍能解析；改完立刻生效。

### 步骤 5（P1）刷新链路

| 位置 | 改动 |
|---|---|
| `internal/skill/registry.go` | 新增 `EnsureFresh()`：对用户目录、共享目录、各原生目录、各仓库根、项目 `.agents/skills` 取目录戳，与扫描时记录的比对，不一致就 `ScanAll`；2 秒节流 |
| `internal/skill/registry.go` | `scanProject` 的缓存从「永久」改为「带目录戳校验」；目录刚出现时能被发现 |
| `internal/skill/registry.go` | 在 `InjectedForProject`、`AllForProject` 入口调用 `EnsureFresh()`（在取锁之前，避免重入） |
| `web/src/composables/useSkillsState.ts` | `ensureSkillsLoaded` 记录加载时间，超过 30 秒再次触发时重拉；`resetSkillsCache` 同时清时间 |

测试：
- 临时目录里新增/删除一个技能目录，不调任何重扫接口，下一次 `InjectedForProject` 就能看到/看不到。
- 目录戳比对要显式设置 mtime（`os.Chtimes`），否则秒级粒度的文件系统会让「刚写就看」出现假阴性。
- 节流窗口内不重复读目录（用计数器断言）。

已知限制（写进注释）：只校验来源根目录的直接子项变化。分类目录里新增技能（`skills/分类/名字/`）不会自动发现，需要重扫；克隆仓库的更新走 pull 触发，不受影响。

### 步骤 6（P2）跳过原因可见

| 位置 | 改动 |
|---|---|
| `internal/skill/scanner.go` | `ScanDir` 返回 `([]Skill, []Skipped)`；`Skipped{Path, Reason}` 只收「有 SKILL.md 但解析失败」的目录（缺 description、YAML 错、名字与目录不符另记） |
| `internal/skill/registry.go` | 按来源保存 skipped，`ScanAll` 一起换掉 |
| `internal/handler/skills.go` | `GET /api/skills` 加 `skipped: [{path, reason}]` |
| `web/src/components/settings/SkillsDiscoveredSetting.vue` | 列表下方折叠区「未加载的技能（N）」，显示路径与原因 |

测试：缺 description 的目录出现在 skipped 里且原因可读；合法技能不进 skipped。

### 步骤 7（P2）同名生效标记

| 位置 | 改动 |
|---|---|
| `internal/skill/registry.go` | 新增 `EffectivePaths(projectPath) map[string]string`（名字 → 生效技能路径），复用 `sourceLess` |
| `internal/handler/skills.go` | 每行加 `shadowed_by`（被谁覆盖，空表示生效） |
| `web/src/components/settings/SkillsDiscoveredSetting.vue` | 被覆盖的行淡显 + 「被 X 覆盖」标签 |

测试：两个同名技能，只有一行 `shadowed_by` 为空；标记结果与 `#name` 实际解析到的路径一致（这是它存在的唯一意义）。

### 步骤 8（P2）符号链接目录

| 位置 | 改动 |
|---|---|
| `internal/skill/scanner.go` | `scanTree` 遇到符号链接：`os.Stat` 跟随后若是目录**且直接含 `SKILL.md`**，按技能收录；不递归进符号链接目录 |

这条规则同时覆盖真实用法（`ln -s ~/dev/myskill ~/.claude/skills/myskill`）和风险（不跟随符号链接容器，天然无环，也不会顺着链接扫到整棵树）。

测试：链接到外部技能目录能发现；链接到普通容器目录被忽略；断链忽略；Windows 下创建符号链接需要权限，用例按平台跳过。

## 五、契约与文档同步

- `internal/api/openapi.yaml`：`/api/skills` 新增 `project_auto_inject`、`disabled`、`skipped`、每行 `injected`/`shadowed_by`。
- `docs/user-guide/configuration.md`：`skills.project_auto_inject`、`skills.disabled` 两行。
- `docs/spec/core/ai-backend.md`：技能章节补「项目技能默认不注入」「优先级顺序」「刷新用目录戳」「`/` 与 `#` 的分工」。
- i18n：`zh.ts` / `en.ts` 增加开关文案、跳过原因标题、覆盖标签；`skillsSource*` 的顺序说明跟着改。

## 六、验证

```
go test ./internal/skill/... ./internal/handler/... ./internal/model/...
npx vitest run web/src/composables/__tests__/skillSourcePriorityParity.test.ts \
               web/src/components/settings/__tests__/SkillsDiscoveredSetting.test.ts
```

手工：
1. 开一个带 `.agents/skills` 的仓库，确认默认不注入（看系统提示或问模型「你能看到哪些技能」）。
2. 打开开关，确认注入恢复。
3. 终端里往 `~/.agents/skills` 放一个技能，不点重扫，等 2 秒后在聊天里按 `#` 应能看到。
4. 造两个同名技能，确认列表标出被覆盖的那一行，且 `#name` 解析到的就是标记为生效的那个。

## 七、风险

- **行为变更**：默认关掉项目注入，老用户会感觉「我的技能不见了」。发布说明必须写，设置页开关的描述要能自解释。
- **目录戳的粒度**：秒级文件系统上可能漏一次变化，靠 2 秒节流 + 下一次请求兜底；极端情况仍可手动重扫。
- **截断 200 字**：极少数技能靠长描述表达触发条件，可能削弱自动匹配。`#` 引用不受影响。
- **优先级对调**：若有人依赖「项目覆盖用户」来给不同仓库定制同名技能，行为会变；这类用法建议改用不同技能名。

## 附：本轮不修的问题

- `ScanAll` 会清空项目缓存，重扫后每个项目第一轮要重走目录（成本可接受）。
- 注入缓存按「智能体 × 项目」增长，无上限（单进程内，量级很小）。
- 没有版本/来源溯源（接 SkillHub 时一起做）。

## 附二：发布注意

1. **项目技能默认不再自动注入**。老用户升级后会感觉「项目里的技能不见了」——需要在设置 → 技能里打开「项目技能自动注入」。这是刻意的默认值，不要为了兼容改回 true。
2. **同名技能的优先级变了**：用户目录现在高于项目目录。若有人依赖「项目覆盖用户」给不同仓库定制同名技能，行为会变，应改用不同技能名。
3. **`/api/skills` 多了四个字段**（`project_auto_inject`、`disabled`、`skipped`、每行 `shadowed_by`），旧客户端忽略即可。

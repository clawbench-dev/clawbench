# 自升级在「运行中被 npm 替换」后失败 — 根因分析与修复方案（E + 版本短路）

> 状态：**方案待评审，尚未实施**
> 日期：2026-09-12
> 分支：`fix/upgrade-self-path`
> 关联：`docs/clawbench-upgrade-stuck-analysis.md`（2026-09-07，supervised 误判问题，与本文不同）

---

## 1. 现象

服务运行期间执行过一次全局 npm 操作后，通过 Web 面板升级会稳定失败：

```
Failed to backup binary: open
/root/.nvm/versions/node/v25.7.0/lib/node_modules/@xulongzhe/.clawbench-dS2lu9p5/node_modules/@xulongzhe/clawbench-linux-x64/bin/clawbench:
no such file or directory
```

日志表现为：走到 `upgrade: current binary` 之后**没有** `upgrade: backup created`，随后失败。

复现三次（同一原因，非偶发）：

| 时间 | current → latest | 结果 |
|---|---|---|
| 2026-09-11 21:32 | v0.92.0 → 0.93.0 | 失败 |
| 2026-09-12 00:32 | v0.92.0 → 0.93.0 | 失败 |
| 2026-09-12 00:34 | v0.92.0 → 0.93.0 | 失败 |

对照：2026-09-10 06:53 升级成功，日志有 `upgrade: backup created`。

---

## 2. 根因

### 2.1 触发条件（三个必须同时满足）

1. 服务**正在运行**（进程已绑定旧 inode）
2. 有人用 npm 对**同一个包目录**执行 `install` / `update`
3. npm 走 retire 替换流程

### 2.2 npm 的 retire 三步

npm 覆盖已存在的包时不原地改写，而是（`@npmcli/arborist/lib/retire-path.js`）：

```
(1) mv  @xulongzhe/clawbench  →  @xulongzhe/.clawbench-<sha1前8位>
(2) 新建 @xulongzhe/clawbench，写入新版本（新 inode）
(3) rm -rf @xulongzhe/.clawbench-<hash>
```

实测确认目录名规则：`sha1('/root/.nvm/.../lib/node_modules/@xulongzhe/clawbench')`
前 8 位 base64 安全字符 = **`dS2lu9p5`**，与报错路径中的 `.clawbench-dS2lu9p5` 完全一致。

### 2.3 为什么进程"活着却找不到自己"

Linux 下进程绑定的是 **inode**，不是路径。且 `/proc/self/exe` 是内核**实时解析**的：

| 阶段 | `argv[0]`（启动时写死） | `os.Executable()`（实时） |
|---|---|---|
| 正常 | `.../@xulongzhe/clawbench/...` | `.../@xulongzhe/clawbench/...` |
| 第 (1) 步后 | **不变** | `.../@xulongzhe/.clawbench-dS2lu9p5/...` |
| 第 (3) 步后 | **不变** | `.../.clawbench-dS2lu9p5/... (deleted)` |

第 (3) 步删除退休目录后，旧 inode 的 `links` 归零（实测 `links=0`），但进程仍持有它继续运行。

**Go 的 `os.Executable()` 会主动剥掉 `" (deleted)"` 后缀**
（`$GOROOT/src/os/executable_procfs.go:27`），返回一个**语法正常、物理不存在**的路径，
且 **`err == nil`**。这就是它如此隐蔽的原因。

### 2.4 失败发生在"读取"，不是"删除"

```go
// internal/service/upgrade.go
369:  currentBin, err := os.Executable()      // err == nil，返回幽灵路径
427:  backupPath := currentBin + ".bak"
428:  if err := copyFile(currentBin, backupPath); err != nil {   // os.Open(源) 失败
429:      SetUpgradeError(fmt.Sprintf("Failed to backup binary: %v", err))
```

`copyFile` 第一步 `os.Open(src)` 即 ENOENT，**从未走到替换/删除**，`.bak` 也从未被创建。

### 2.5 最小复现（已实测）

启动一个进程后，按 npm 三步 retire 它的包目录，再让它读一次自身路径：

```
[start ] os.Executable() = /tmp/exprepro/pkg/bin/clawbench
  (1) renamed pkg -> .clawbench-HASH
  (2) installed NEW pkg/bin/clawbench (inode=..., size=26936)
  (3) deleted .clawbench-HASH
[after ] os.Executable() = /tmp/exprepro/.clawbench-HASH/bin/clawbench
[after ] os.Open(exe)    = ERROR: no such file or directory
```

**即使原路径 `/tmp/exprepro/pkg/bin/clawbench` 又存在（新装的），`os.Executable()` 也不会返回它。**

### 2.6 为什么 UI 升级本身不会自伤

| | UI 升级 | npm 更新 |
|---|---|---|
| 替换粒度 | **文件级** `rename(new → bin/clawbench)` | **目录级** `mv clawbench → .clawbench-hash` |
| 目录名 | 不变 | 改名 + 删除 |
| 进程的路径 | **始终有效** | **彻底消失** |

UI 升级替换完文件后，原路径仍指向新文件；npm 则让路径本身消失。**这就是"别人能升级、你不能"的全部差别。**

---

## 3. 为什么"运维规避"不是修复

曾考虑的三种做法及其缺陷：

| 方案 | 隐含假设 | 为什么不够 |
|---|---|---|
| A 预检 + 友好报错 | 用户看到提示会去重启 | 升级仍然**失败**，只是文案好看 |
| B 读 `/proc/self/exe` 备份 | — | 只能解决"读自己"，**解决不了"替换到哪"**（目标路径已不存在） |
| C 文档约束 | 用户会看文档并遵守 | 无法保证；本问题 2026-09-02 已踩过一次并记录，9-11 再次复发 |

**共同缺陷：都建立在"用户会配合"的假设上。** 使用方可能只通过 npm 下载、用 UI 升级，也可能在后台 npm 升级，行为不可控。

**真正的修复目标：让程序不依赖那个会被外部工具改掉的路径。**

---

## 4. 修复方案 E：启动时固化自身路径 + 版本短路

### 4.1 核心思路

`{data-dir}`（默认 `~/.clawbench`）**不在 npm 的包目录内**，npm 永远不会触碰它。
因此在**启动时**（此刻二进制必然存在，`os.Executable()` 可信）把自身**绝对路径**固化下来：

```
{data-dir}/self-path     内容示例：
/root/.nvm/.../node_modules/@xulongzhe/clawbench-linux-x64/bin/clawbench
```

升级时优先读它，而不是直接信任 `os.Executable()`。

### 4.2 为什么选 E 而不是 D（argv[0] 回退）

曾考虑 `os.Executable()` 失效时回退到 `os.Args[0]`。实测四种启动方式：

| 启动方式 | `argv[0]` | 能否直接使用 |
|---|---|---|
| npm 包装器 `spawn(binPath)` | 绝对路径 | 可以 |
| 终端 `./clawbench` | `./clawbench` | **需配合 cwd 解析** |
| 终端 `clawbench`（PATH） | `clawbench` | **需自行搜 PATH** |
| `exec -a` 自定义 | 任意 | **可被伪造** |

而 `os.Executable()` 在四种方式下**均返回绝对路径**（已实测）。

**结论：D 是 E 的一个更弱版本** —— 两者指向同一路径，但 D 引入 cwd 依赖与 argv[0] 可伪造两个额外风险，
且无法覆盖"E 的文件意外丢失"以外的任何新场景。**不采用 D。**

### 4.3 版本短路优化（用户明确要求）

npm 升级到 v0.93.0 后，磁盘上其实**已经是目标版本**，UI 升级没必要再下载一遍 42MB 的 tgz。

**短路逻辑：** 若磁盘上的二进制版本已 ≥ registry 的 latest，则跳过"下载 + 备份 + 替换"，
直接进入重启阶段（让新版本被真正加载）。

### 4.4 改动清单

| # | 文件 | 改动 |
|---|---|---|
| 1 | `internal/service/selfpath.go`（新增） | `WriteSelfPath()` / `ReadSelfPath()`；`{data-dir}/self-path` 读写，0600 权限 |
| 2 | `cmd/server/main.go`（~L415 之后） | `model.DataDir` 确定后调用一次 `WriteSelfPath()` |
| 3 | `internal/service/upgrade.go:369` | `os.Executable()` → `resolveSelfBinary()`（读固化路径 → 校验 → 回退 `os.Executable()`） |
| 4 | `internal/service/upgrade.go:770`（`CheckInstallDirWritable`） | 同样改用 `resolveSelfBinary()` |
| 5 | `internal/service/upgrade.go`（下载后 / 备份前） | 插入版本短路判断 |
| 6 | `internal/service/upgrade_state.go` | 新增错误码 `UpgradeErrSelfPathUnresolved` |
| 7 | `web/src/composables/useUpgrade.ts` | 导出 `ERR_SELF_PATH_UNRESOLVED` |
| 8 | `web/src/components/settings/UpgradeDialog.vue` | 新增该 code 的展示分支 |
| 9 | `web/src/i18n/locales/{zh,en}.ts` | 新增文案 key |
| 10 | 测试 | 见 §6 |

### 4.5 `resolveSelfBinary()` 逻辑

```
1. 读 {data-dir}/self-path
   存在且 os.Stat 成功 → 返回它                      ← npm 场景走这里
2. 回退 os.Executable()
   os.Stat 成功 → 返回它                              ← 正常场景 / 首次升级
3. 两者都失败 → 返回错误（触发 UpgradeErrSelfPathUnresolved）
```

> **注意**：升级**成功后**应把新的自身路径重新写回 `self-path`，
> 否则替换后记录的路径可能与实际不符（虽然通常路径不变，但 Docker/容器场景可能不同）。

### 4.6 版本短路逻辑

```
下载 tgz 并解压出 newBinPath（复用现有 downloadAndExtract）
        ↓
读取磁盘上 selfBin 的版本（执行 `<selfBin> --version`，或读 newBinPath 的版本）
        ↓
if 磁盘版本 >= info.LatestVersion:
        跳过备份/替换，直接进入重启阶段
        （若需重启，走现有 sentinel / upgrade-replace 机制）
else:
        走原有备份 → 替换 → 重启流程
```

> **待评审点**：短路时"重启"的具体方式需要与现有 supervised / unsupervised / container 三条路径对齐，
> 可能引入复杂度。**备选**：短路只跳过"下载"，仍执行备份+替换（因替换成本很低，且能保证一致性）。

### 4.7 边界场景

| 场景 | 行为 |
|---|---|
| 正常 UI 升级 | 读 self-path（有效）→ 正常升级 ✅ |
| **npm 重装后点 UI 升级** | self-path 指向原路径，npm 已把新版填回 → **有效，升级成功** ✅ |
| 首次升级（self-path 不存在） | 回退 `os.Executable()` ✅ |
| 安装目录被手动 `mv` | self-path 与 `os.Executable()` 均失效 → 明确报错（此时"原地替换"本就无合理目标） |
| `{data-dir}` 不可写 | 写 self-path 失败 → 记录 WARN，不阻断启动；升级时回退 `os.Executable()` |
| 容器内 | self-path 落在挂载的 data 卷，行为一致 |

---

## 5. 明确不做的事

- **不采用方案 D**（argv[0] 回退）—— 理由见 §4.2
- **不采用方案 B**（`/proc/self/exe` 备份）—— 治不了"替换目标消失"的根，且跨平台需分支
- **不改动 sentinel 的路径获取逻辑**（`settings_sentinel_unix.go:17`）——
  哨兵在**重启**场景运行，此时进程刚被替换、路径正常，风险低。
  （若评审认为需要，可一并改用 `resolveSelfBinary()`，但会引入 `handler → service` 的依赖考量。）

---

## 6. 测试要点

按 AGENTS.md：功能与修复必须含单元测试，变更行覆盖率 ≥ 80%。

### Go

1. `WriteSelfPath` / `ReadSelfPath` 往返正确；文件权限为 0600
2. `resolveSelfBinary`：
   - self-path 存在且有效 → 返回 self-path
   - self-path 存在但 `os.Stat` 失败 → 回退 `os.Executable()`
   - self-path 不存在 → 回退 `os.Executable()`
   - 两者皆失效 → 返回错误
3. **回归**：`performUpgrade` 在 self-path 有效时，备份/替换流程与改动前一致
4. 版本短路：磁盘版本 ≥ latest 时跳过下载（或按 §4.6 的最终决定验证对应行为）
5. `CheckInstallDirWritable` 使用 `resolveSelfBinary` 后行为不变（回归）

### 前端

6. `error_code === 'self_path_unresolved'` 时渲染对应文案分支
7. i18n key 在 zh/en 均存在（参照现有 `navKeys.test.ts` 风格的 key 完整性测试）

---

## 7. 待评审确认的问题

1. **版本短路时如何"重启"？** 三条重启路径（supervised / upgrade-replace / container）如何复用？
   还是短路只跳过下载、仍走完整替换（更简单、更一致）？
2. **升级成功后是否回写 self-path？** §4.5 认为应该，请确认。
3. **哨兵是否一并改用 `resolveSelfBinary`？** §5 暂定不改，请确认。
4. **`{data-dir}/self-path` 的文件名**是否合适？（备选：`self-bin-path`、`.self-path`）

---

## 8. 验证方式（实施后）

1. `go test ./internal/service/... ./internal/handler/...`
2. `npm test`（前端）
3. `./scripts/pre-push-checks.sh`
4. **端到端手工验证**：服务运行中执行一次全局 npm 操作 → 点 UI 升级 → 应成功（这是本方案的核心验收）

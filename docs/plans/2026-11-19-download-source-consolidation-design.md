# 下载源收口 + 桌面全量包简化设计

日期：2026-11-19
状态：已实施（2026-11-19）；实现差异见 §12.1
相关文档：[桌面端客户端](../spec/client/desktop-client.md)、[版本与发布](../spec/infra/versioning.md)

---

## 0. 摘要

一句话：**把“从哪下载”收成服务端的一份策略；桌面只发/只下全量包；智能体和后端包共用同一份 npm 源列表，用户自己配置的源优先。**

四个改动：

1. **桌面删除载荷包（payload）整条链**，只保留全量包（约 150MB）。发布、API、客户端安装、CI、npm 载荷包一起删。
2. **新增 Go 源策略包**。CLI 自升级、桌面包地址、智能体安装共用同一份 npm/GitHub 源顺序；Node 二进制顺序仍由 `platform` 提供（层级原因，见 §12.1），但不再有第二份 npm 源逻辑。删掉现在散落的 4 份镜像常量和 5 处选源代码。
3. **智能体安装改成“先探源、再装一次”**。源顺序与后端包一致：用户配置的 npm 源 → 地区镜像 → 官方源。不再无条件追加 npmmirror。
4. **桌面全量包补 SHA256 校验**。现在全量包只有 https，没有校验和；删掉载荷后，不能把唯一的完整性校验一起丢掉。

**不升协议号。** 旧客户端连新服务端、新客户端连旧服务端都能工作，详见 §7。

**待定：** 全量包在国内从哪托管（GitHub + gh-proxy，还是国内对象存储/CDN），见 §8。

---

## 1. 现状：现在实际在跑的六条链路

### 1.1 六条链路

| 链路 | 谁选源 | 现在的顺序 | 失败怎么办 | 证据 |
|---|---|---|---|---|
| 服务端自升级（npm 包） | 服务端 | 地区镜像 + 服务端本机 `~/.npmrc`/环境变量 | 查元数据逐个试；下载只试选中的那一个 | `internal/service/upgrade.go:219-294`、`:332`、`:653` |
| 桌面客户端升级 | 服务端排 URL，客户端执行 | 全量包：GitHub 镜像按**服务端**地区排序；载荷包：npm 按服务端地区 + 客户端本机 npmrc 兜底 | 客户端逐个试；载荷失败静默回落全量 | `internal/service/desktop_upgrade.go:116-255`、`desktop/src/main/install.ts:574-760` |
| 智能体安装 | 服务端 | 只对 `npm install` 追加地区镜像 | 没有回退 | `internal/handler/agent.go:55-63`、`internal/handler/agent_install.go:63` |
| Node 运行时 | 服务端 | 地区镜像 ↔ 官方 | 逐个试 | `internal/platform/node_runtime.go:257-266` |
| Android APK | 当前服务器 | 没有多源 | 无 | `internal/handler/apk.go`、`android/.../MainActivity.java:1922` |
| 一键安装脚本 | 脚本自己 | 先 ping npmmirror，通则镜像，不通回官方 | 镜像 → 官方 | `scripts/install.sh:264-270` |

### 1.2 现在的问题

- **源策略散在 5 处**：Go 服务端、Electron 客户端、shell 脚本、Android、CI。镜像地址常量复制了 4 份：`internal/platform/network.go:204`、`internal/service/upgrade.go:221`、`internal/service/upgrade_signature.go:35`、`internal/handler/agent.go:36`。`platform.NpmMirrorRegistry` 定义了但没人用。
- **智能体安装和后端包不一致**：后端包会读服务端本机 npmrc、会多源回退；智能体安装只追加一个 `--registry`，不读用户配置，也没有回退。用户配了私有源也会被 `--registry` 盖掉。
- **后端包下载只试一个地址**：元数据阶段会试多个源，但 tarball 只用“查成功那个源”的地址；这个地址坏了就整次升级失败。
- **全量包没有校验和**：只剩 https。载荷包反而有 npm `dist.integrity`；删掉载荷后，桌面升级会失去唯一的完整性校验。
- **载荷包带来 6 类复杂度**：npm 载荷包发布、壳指纹、Electron ABI 校验、克隆安装、integrity 查询、失败双错误展示、CI 交叉断言、跨语言包名守护。删掉它是这次简化的主要收益。

---

## 2. 目标与非目标

### 2.1 目标

- 源选择**只在服务端做一次**；客户端只消费排好序的地址，不再各自维护一份。
- 智能体安装与后端包共用同一份 npm 源列表，**用户显式配置的源优先**。
- 桌面只发布、只下载全量包；删除载荷包整条链。
- 全量包有可验证的 SHA256；校验失败能明确报错，不静默装坏包。
- 旧客户端/新服务端、新客户端/旧服务端都能工作，不强制升级。

### 2.2 非目标

- 不做多源并发测速、不做运行时热切换源。
- 不做第三方插件市场、不做远程加载代码。
- 不改 Android 的 APK 分发方式（它只从当前服务器下，没有多源）。
- 不为旧载荷客户端保留载荷代码。旧客户端连新服务端时自然走全量。
- 不做发布签名（release signing）。SHA256 只解决传输损坏和镜像被篡改；更强的签名链是另一件事。

---

## 3. 源策略包

### 3.1 位置与职责

新增 `internal/platform/source`（`platform` 的子包）：

- 只做**纯决策**：地区判断、候选顺序、URL 校验、URL 拼接。
- 不做网络请求，不执行 `npm`，不下载。
- 需要读环境变量和用户级 `~/.npmrc`；测试用包级变量注入（和现有 `platform` 的 `probeChinaDialer` 做法一致）。
- **必须自带 `_test.go`**。新包的覆盖率按包统计，不能指望 `service` 包的测试顺带覆盖（仓库已有先例：抽包后 Tier 2 覆盖率塌陷）。

`source` 可以 import `platform`（用 `platform.IsChinaMainland()`、`platform.UserHomeDir()`），`platform` 不 import `source`，没有环。

### 3.2 接口

```go
package source

// 两个公开 npm base。签名语义留在 service，source 只提供地址常量，
// 避免“从哪下”和“该不该验签”混在一起。
const (
    NpmJsBase     = "https://registry.npmjs.org"
    NpmMirrorBase = "https://registry.npmmirror.com"
)

// Candidate 是一个下载源。Name 只用于日志和错误信息，Base 是规范化后的地址。
type Candidate struct {
    Name string // "user" / "region-mirror" / "official" / "gh-proxy" / "ghfast" / "direct"
    Base string // 不带结尾斜杠；GitHub 直连时为空
}

// regionProbe 是包级变量，测试可替换，避免单测依赖外网。
var regionProbe = platform.IsChinaMainland

// Region 返回是否在中国大陆。
func Region() bool

// Npm 返回 npm registry 的候选顺序：用户配置 → 地区镜像 → 官方，去重。
func Npm() []Candidate

// ReleaseURLs 返回一个 GitHub Release 资产的完整候选 URL：
// 国内 gh-proxy → ghfast → 直连；海外 直连 → gh-proxy → ghfast。
// repo 形如 "owner/name"，tag 形如 "v0.130.0"，asset 是资产文件名。
func ReleaseURLs(repo, tag, asset string) []string

// UserNpmRegistry 返回用户显式配置的 npm 源，没有则空。
// 顺序：NPM_CONFIG_REGISTRY → npm_config_registry → 用户级 ~/.npmrc 的 registry=。
func UserNpmRegistry() string

// URL 把 Base 和路径拼起来，保留 Base 自带的路径前缀，避免出现 "//"。
func (c Candidate) URL(path string) string
```

`ReleaseURLs` 负责拼完整地址（`https://github.com/<repo>/releases/download/<tag>/<asset>`），调用方不再自己拼；`repo` 由调用方传入（`desktopReleaseRepo` 留在 `service`）。前缀代理是内部实现，不导出。

### 3.3 具体规则

**npm 候选（`Npm()`）**

1. 用户配置：`NPM_CONFIG_REGISTRY` → `npm_config_registry` → 用户级 `~/.npmrc` 的 `registry=`。
2. 地区默认：国内 `https://registry.npmmirror.com`，海外 `https://registry.npmjs.org`。
3. 另一个：国内补 `npmjs`，海外补 `npmmirror`。
4. 去重；非法值跳过并记日志。
5. 校验：只接受 https；http 只允许本机回环（`localhost` / `127.0.0.1` / `::1`）。这和桌面端现有的校验规则对齐，避免用户配了 `http://` 私有源后升级走明文。
6. **用户源优先**：用户配了就用第一个；没配才用地区镜像。地区镜像之后还有官方源兜底（现在国内没配用户源时只有 npmmirror，镜像挂了没有官方兜底）。
7. **tarball 改写只对公开镜像生效**：查元数据时如果响应里的 `dist.tarball` 指向 `registry.npmjs.org`，把它改写到**地区公开镜像**（国内 npmmirror，海外 npmjs）；**绝不改写到用户私有 base**。用户源返回自己的 tarball 地址时原样使用。这样用户源只代理元数据、不代理 tarball 时，下载会走地区镜像，而不是打到一个没有这个包的地址。
8. **401/403 分链路处理**：
   - **CLI 自升级（元数据阶段）**：服务端自己发 HTTP 请求，拿不到交互式 npm 凭证；用户源 401/403 时记日志并继续下一个候选，不能让一次鉴权失败把自升级卡死。
   - **CLI 自升级（tarball 阶段）**：如果用户源要求 tarball 也带鉴权，服务端不复用 npm 的 `_authToken`，下载会失败；这时明确报错，提示改用公开镜像或手动升级，**不静默绕过**。
   - **智能体安装**：安装命令跑的是 `npm`，npm 会读服务端 `~/.npmrc` 里的凭证；用户源 401/403 就是真失败，停止探测、用该源装、把错误给用户，不绕过。
9. **http 私有源**：只允许本机回环的 http；其他 http 源跳过并记警告。这是一次行为变化（现在服务端接受任意 http），发布说明里要写。

**GitHub Release 候选（`ReleaseURLs()`）**

- 国内：`https://gh-proxy.com/` → `https://ghfast.top/` → 直连。
- 海外：直连 → `https://gh-proxy.com/` → `https://ghfast.top/`。
- 这是现有 `githubReleaseMirrors` 的顺序，只是搬进 `source`，并按地区排序。
- 只用于 GitHub Release 资产（全量包、融合包）。npm tarball 绝不能改写到这些前缀上。

**Node 候选（留在 `platform.NodeDownloadBases()`）**

- 国内：`https://registry.npmmirror.com/-/binary/node` → `https://nodejs.org/dist`。
- 海外：反过来。
- Node 不是 npm 包，用户 npm 源不参与。

### 3.4 调用方改造

| 现在 | 改成 |
|---|---|
| `service/upgrade.go` 的 `getRegistryBase` / `getUserRegistryBase` / `isValidRegistryURL` / `parseNpmRcRegistry` / `registryCandidates` | 删掉，改用 `source.Npm()` |
| `service/upgrade_signature.go` 的 `isOfficialRegistry` / `npmMirrorRegistryBase` | 判断逻辑留在 service；两个 base 常量改用 `source.NpmJsBase` / `source.NpmMirrorBase`。签名语义不搬进 `source` |
| `service/desktop_upgrade.go` 的 `githubReleaseMirrors` / `releaseAssetURLs` | 前缀顺序和 URL 形状改用 `source.ReleaseURLs(desktopReleaseRepo, ...)`；repo 常量留在 `service` |
| `platform/node_runtime.go` 的 `nodeDownloadBases` | 保持不变（`platform` 不能反向 import `source`） |
| `handler/agent.go` 的 `npmMirrorRegistry` / `prepareInstallCmd` | 拆成“显示用”和“安装时解析”两个函数，见 §4 |
| 4 份镜像常量 | 只留 `source` 里的一份 |

### 3.5 收口后的源顺序

| 要下载的东西 | 国内顺序 | 海外顺序 |
|---|---|---|
| 服务端 CLI npm 包 | 用户源 → npmmirror → npmjs | 用户源 → npmjs → npmmirror |
| 智能体 CLI npm 包 | 同上 | 同上 |
| 桌面全量包 | gh-proxy → ghfast → github | github → gh-proxy → ghfast |
| Node 运行时 | npmmirror 二进制 → nodejs.org | nodejs.org → npmmirror 二进制 |
| Android APK | 当前服务器 | 当前服务器 |

**统一原则：选源只选一次；能拿到多个候选 URL 的下载可以逐个试；安装只执行一次。**

- 桌面全量包、Node 运行时能拿到候选列表，逐个试。
- CLI 自升级当前只拿到一个 tarball URL（元数据阶段已经选了源），本次不改下载重试；这是已知差异，不是“统一”漏掉。以后要加也可以，但不在这次范围。
- 智能体安装只装一次；失败不换源重装。

---

## 4. 智能体安装

### 4.1 显示和执行分开

现在 `prepareInstallCmd` 同时用于列表展示和执行（`internal/handler/agent.go:475`、`:2564`、`agent_install.go:63`）。加了“探源”之后不能这样：列表页不能为了展示一个命令去等网络。

拆成两个函数：

- `displayInstallCmd(cmd)`：只用于列表/详情展示，不探测。**只对 `npm install` 开头的命令**追加 `--registry`；`npm i`、`npx`、`curl | bash` 原样返回。按 `source.Npm()` 的第一个候选追加；如果第一个是 npmjs 官方源，就不追加（npm 默认就是它）。
- `resolveInstallCmd(ctx, cmd)`：只在真正安装时调用。按 `source.Npm()` 逐个探测，返回实际要执行的命令。

这里的 `source.Npm()` 读的是**服务端机器**的配置，因为安装命令就在这台机器上执行。这和桌面升级（读客户端机器的 npmrc）刻意相反，参见 `docs/spec/client/desktop-client.md:152`。不要把两者“统一”成同一个视角。

列表里显示的命令和最终执行的命令在极少数情况下可能不同（探测时用户源挂了，落到地区镜像）。安装结果里带 `Command` 字段，UI 展示结果时以它为准。

### 4.2 探测规则

1. 只处理 `npm install` 开头的命令；`npm i`、`npx`、`curl | bash` 保持原样（没有通用的源参数，也不要改写用户能直接复制的命令）。
2. 命令里已经有 `--registry` 就不动，尊重调用方/后端的显式选择。
3. 从命令里取出包名：只按 `npm install -g <pkg>` 精确切词，**不把整条命令交给 shell 解析**（Windows 走 `cmd /C`，更不能拼字符串）。只探测单个 registry 包名；本地路径、tarball、git URL 不探测，直接用第一个候选装。
4. 按 `source.Npm()` 的顺序，对每个候选执行：
   ```
   npm view <pkg> version --registry=<base>
   ```
   每个 3 秒超时（与后端包查元数据一致）。探测和安装都在**执行安装的那台机器**上跑（`InstallAgent` 本来就是本机 shell）。
5. “成功”= `npm view` 退出码 0 且输出了非空版本号；超时、非零、输出为空都算失败。第一个成功的源就是这次安装的源。
6. **401/403 不绕过（这是本意，不是隐患）**：用户源明确拒绝（需要登录/没权限）时，停止探测，直接用这个源装，把 npm 的真实错误给用户。只有“这个源没有这个包（404）”或“连不上”才继续下一个。
   - 用户配了私有源，就是要去那里装；匿名访问被拒时绕到公共镜像，反而违背了用户的配置。
   - 这和 CLI 自升级的 401 处理不同：CLI 升级是服务端自己出站、拿不到交互式凭证，所以回落；智能体安装跑的是 `npm`，npm 会读服务端 `~/.npmrc` 的凭证，401 就是真失败。
7. 所有源都探不到：用第一个候选直接装，把 npm 的真实错误给用户；不要让探测本身挡住安装。
8. 安装只执行一次，带选中的 `--registry=<base>`；安装失败不换源重装。
9. 安装结果和错误信息里写明实际使用的源，方便排查。

### 4.3 Node 运行时

- 保持现有行为；候选顺序仍由 `platform` 提供，不进 `source`。
- 缺 npm/npx 时自动装私有 Node；校验 SHASUMS，拿不到就跳过（与现在一致）。
- 用户 npm 源不参与 Node 二进制下载。

### 4.4 为什么不是“每个源都装一遍”

- 安装不是幂等操作：`npm install -g` 失败可能留下半装的包、写了一半的全局目录。再换一个源重装，可能把问题叠加。
- 探测的成本是一次 `npm view`（3 秒），比装一遍便宜得多。
- 所以：**先用元数据探源，再只装一次**。这和后端包“先查元数据、再下载”的形状一致。

---

## 5. 桌面：只保留全量包

### 5.1 改后链路

1. 客户端启动或手动检查 → `GET /api/desktop/latest`。
2. 服务端返回：`version`、`clientVersion`、`protocol`、`tag`、`downloads`（每个平台一组排好序的 URL）、`checksums`（每个资产文件名的 SHA256）。
3. 客户端按 `downloads` 的顺序逐个下载；带现有看门狗（30KB/s 或 20 秒无进展就换下一个）。
4. 下载完按 `checksums` 校验；通过后解压到 `app-<version>`，原子改名，切 `current` 指针，问用户是否重启。
5. macOS 和现在一样走全量；Windows 融合版走 `fused_downloads` + `fused_checksums`，其余不变。

不再有：载荷包、壳指纹、Electron 主版本比对、克隆安装、npm integrity 查询、载荷失败的双错误展示。

### 5.2 删除清单

#### Go / API

| 文件 | 删除/修改 |
|---|---|
| `internal/service/desktop_upgrade.go` | 删 `desktopPayloadAssetBase`、`desktopPayloadNpmPkg`、`desktopPayloadNpmURLs`、`desktopPayloadURLs`；删 `DesktopLatestResult.Payloads` 字段和 `FetchDesktopLatest` 里的填充；保留 `desktopAssetBase`、`desktopAssetName`、`desktopFusedAssetBase`、`FusedDesktopDownloads`；`releaseAssetURLs` 保留但内部改用 `source.ReleaseURLs()` |
| `internal/service/desktop_upgrade_test.go` | 删载荷相关用例；新增“响应里没有 `payloads`”“有 `checksums`”的用例 |
| `internal/service/upgrade.go` | `FusedDesktopDownloads` 不变；新增 `fused_checksums` 的组装（见 §6） |
| `internal/handler/desktop_test.go` | 两处直接构造 `service.DesktopLatestResult{Payloads: ...}`（`:26`、`:77`），删字段后会编译失败；删掉这些字段和断言，改成断言“没有 `payloads`” |
| `internal/api/openapi.yaml` | `:9494` 起有 `payloads` 的描述，`:9548` 有 schema；整段删除，补 `checksums` / `fused_checksums` |
| `internal/handler/*` | 检查其他透传 `payloads` 的地方；有就删 |

#### Electron 客户端

| 文件 | 删除/修改 |
|---|---|
| `desktop/src/main/install.ts` | 删 `localNpmRegistryBase`、`normalizeRegistryBase`、`isLoopbackHost`、`readNpmrcRegistry`、`npmTarballPath`、`mirrorFallbackUrls`、`urlKey`（如果只用于镜像去重）、`fetchNpmIntegrity`、`packumentUrl`、`NPM_METADATA_TIMEOUT_MS`、`withTimeout`、`PAYLOAD_MANIFEST`、`SHELL_FINGERPRINT_FILE`、`readShellFingerprint`、`PayloadIncompatibleError`、`cloneShell`、`installPayload` 及载荷专用的解压/克隆辅助；保留 `downloadAndInstall`、`downloadFirstAvailable`、`extractArchive`、`versionDir`、`writePointer`、`restartInto` |
| `desktop/src/main/updater.ts` | 删 `DesktopLatest.payloadUrls`、响应接口里的 `payloads`、`fetchDesktopLatest` 里的 `payloadUrls` 解析；新增 `checksums` |
| `desktop/src/main/versionGate.ts` | 删 `VersionGateInfo.payloadUrls`、`installServerVersion` 的载荷分支、`fetchNpmIntegrity`/`installPayload` import、`payloadErr` 和双错误展示；保留全量下载和版本闸门 |
| `desktop/src/main/fusedUpdate.ts` | 不改逻辑；`downloadAndInstall` 增加 checksum 参数 |
| `desktop/src/main/index.ts` | 检查载荷引用；有就删 |
| `desktop/src/main/labels.ts` | 删 `payloadInstallFailed` 和 `fullDownloadFailed`（中英文两份，`:61`、`:91`、`:109`）；删完用 grep 确认零引用 |
| `desktop/src/shared/integrity.ts` + `.test.ts` | 删（只服务载荷包的 SRI 校验，已无生产调用方） |
| `desktop/src/main/install.test.ts` | 删载荷用例；保留全量下载、解压、指针、重启用例；新增 SHA256 通过/失败用例 |
| `desktop/src/main/updater.test.ts` | 删 `payloads` 用例；新增“旧服务端没有 `checksums` 也能解析”的用例 |
| `desktop/src/main/versionGate.test.ts` | 删载荷分支用例；保留全量路径 |
| `desktop/scripts/shell-fingerprint.test.ts` | 删除 |

#### 发布 / npm

| 文件 | 删除/修改 |
|---|---|
| `.github/workflows/release.yml` | 删三个 `Build desktop payload` 步骤（Linux x64、Linux arm64、Windows）、三个 payload artifact 上传、`publish-npm-desktop` job、`shell-fingerprint.mjs` 步骤、payload 目录校验；`reuse-client-artifact.sh` 调用去掉 payload 参数；`sync-npmmirror` 的参数（`:1773-1775`）去掉三个 `-payload` 包；新增 `SHA256SUMS` 生成/上传 |
| `scripts/lib/reuse-client-artifact.sh` | 去掉 `npm_pkg` 和 `payload_dir` 参数、载荷下载/解包分支；只保留复用全量 zip |
| `npm/desktop-payloads/` | 整个目录删除（3 个平台包） |
| `desktop/electron-builder.yml` | `:16`、`:54-55` 的注释讲的是载荷的打包动机，删载荷后改成“薄包只含应用，融合包额外带 server” |
| `desktop/scripts/stage-payload.mjs` + `.test.ts` | 删除 |
| `desktop/scripts/shell-fingerprint.mjs` + `.test.ts` | 删除 |
| `scripts/__tests__/releaseAssets.test.ts` | `:137-197` 的四组载荷/壳指纹用例整段删（不是改断言）；改成反向守护“发布资产/打包命令里不得出现 `-payload`” |
| `scripts/__tests__/npmTrustedPublishing.test.ts` | 删载荷包相关期望 |

#### 文档

| 文件 | 修改 |
|---|---|
| `docs/spec/client/desktop-client.md` | 删“增量升级：只下载载荷包”整节；自升级流程图里去掉载荷分支；`:148-152` 关于 `mirrorFallbackUrls` 的说明整段删（桌面不再读 npmrc） |
| `docs/spec/infra/versioning.md` | 删“桌面端载荷包（增量升级）”整节和相关守护条目 |
| `AGENTS.md` | `:185`（`install.ts` 行的“增量载荷包”）、`:191`、`:193`、`:203`（`publish-npm-desktop` / 载荷包）都要改 |
| `README.en.md` | `:186` 的桌面能力表删掉 “incremental payload” |
| 本文 | 作为这次改动的设计记录 |

删完后跑一遍：

```
grep -rn "desktop-payload\|Payloads\|payloadUrls\|installPayload\|shell-fingerprint\|payload.json" \
  --include='*.go' --include='*.ts' --include='*.mjs' --include='*.yml' --include='*.md' .
```

除本文和反向守护测试外，结果应为空。

### 5.3 保留的东西

- `downloadAndInstall`：全量包下载 + 解压 + 侧装 + 切指针。
- `downloadFirstAvailable`：候选逐个试、https 校验、进度看门狗。
- `fusedUpdate.ts`：融合版只认融合全量包，逻辑不变。
- `versionGate` 的协议比较、版本方向、重启提示。
- `desktop_upgrade.go` 的全量包资产名、平台映射、融合包映射。

---

## 6. SHA256 清单

### 6.1 文件格式

发布时生成一个 `SHA256SUMS` 文件，放在 Release 资产根目录（和全量包并列）。格式沿用 coreutils 的写法：

```
<64 位小写十六进制><两个空格><文件名>
```

示例：

```
9f2c1d...  clawbench-desktop-linux-x64-v0.130.0.zip
4a7e0b...  clawbench-desktop-linux-arm64-v0.130.0.zip
7b3e55...  clawbench-desktop-darwin-arm64-v0.130.0.zip
2d8c90...  clawbench-desktop-darwin-x64-v0.130.0.zip
3e6f11...  clawbench-desktop-windows-x64-v0.130.0.zip
1c9d22...  clawbench-desktop-windows-x64-local-v0.130.0.zip
```

macOS 只有 `darwin-arm64` 和 `darwin-x64` 两个全量包，没有融合包、没有载荷。

约束：

- 一行一个资产；按文件名排序，保证每次生成的文件稳定。
- 只写**文件名**，不写路径；文件名就是 Release 资产的文件名。
- UTF-8、LF、末尾一个换行；没有注释、没有空行。
- 包含全量包和融合包；**不包含**载荷包（已删除），也不包含 `SHA256SUMS` 自己。
- 平台没有该资产就不写这一行（例如 macOS 没有融合包）。

### 6.2 生成

现有 CI 里三个桌面 job 是用 `softprops/action-gh-release` 直接上传 `files:`（`release.yml:896-899`、`:1261-1266`、`:1412-1417`），没有“本地产物”这一步；所以不能只在文档里写一句“最后汇总”，必须把下面几步写死：

1. **每个桌面 job 在 `action-gh-release` 之前**算自己即将上传的 zip 的 SHA256：Linux/macOS 用 `sha256sum`，Windows 用 `Get-FileHash -Algorithm SHA256`。
2. 每个 job 写一个片段文件 `SHA256SUMS.<platform>`，用 `upload-artifact` 上传。**不要复用现有的 `desktop-*-package` artifact**——那里面是 `*-unpacked/` 目录，不是 zip。
3. Windows job 要算两个：薄包和融合包。融合包是在 job 内 `ZipFile` 拼装出来的（`release.yml:1166-1177`），必须在拼装后算；现有 `desktop-windows-package` artifact 里没有这个 zip。
4. **复用路径也要算**：`NEEDS_BUILD != 'true'` 时 job 只跑 `reuse-client-artifact.sh`，但被复用的 zip 就在本地（软链/复制出来的 `desktop/release/*.zip`）。复用 job 必须重新计算它的 SHA256 并上传片段；否则这个平台在官方发布的 `SHA256SUMS` 里完全缺席，客户端会记一条 unverified 然后静默不校验。
5. **汇总 job**：`needs` 四个桌面 job（linux、linux-arm64、windows、macos），下载所有片段，按文件名排序合并成 `SHA256SUMS`。
6. 因为 Release 已经由各 job 建好，汇总 job 用 `gh release upload <tag> SHA256SUMS --clobber` 追加，不能再调一次 `action-gh-release` 去重建。
7. 汇总 job 加一条自检：`SHA256SUMS` 里每一行对应的文件都在 Release 资产里，Release 里每个桌面 zip 都在清单里；缺一个就失败。
8. 如果选了对象存储（§8），同一份 `SHA256SUMS` 也上传到对象存储。

### 6.3 API 形状

`GET /api/desktop/latest` 增加：

```json
{
  "checksums": {
    "clawbench-desktop-linux-x64-v0.130.0.zip": "9f2c1d...",
    "clawbench-desktop-linux-arm64-v0.130.0.zip": "4a7e0b..."
  }
}
```

`GET /api/upgrade/check` 增加（融合版用）：

```json
{
  "fused_checksums": {
    "clawbench-desktop-windows-x64-local-v0.130.0.zip": "1c9d22..."
  }
}
```

为什么按**文件名**而不是按平台：

- 同一个资产可能从多个镜像地址下载（gh-proxy、ghfast、直连），文件名不变，平台键也不变；按文件名可以直接从 URL 的最后一段查。
- 两个 map 都按文件名键，格式相同；全量包放在 `/api/desktop/latest`，融合包放在 `/api/upgrade/check`，不用为每种包发明不同的结构。
- 旧客户端不认识这两个字段，会忽略；新客户端遇到旧服务端没有字段，就跳过校验。

### 6.4 服务端怎么拿到清单

- 服务端在**第一次**收到该 tag 的 `/api/desktop/latest` 或 `/api/upgrade/check` 时，从 GitHub **直连**拉 `SHA256SUMS`：
  ```
  https://github.com/<repo>/releases/download/<tag>/SHA256SUMS
  ```
  3 秒超时；成功后按 tag 缓存在内存里（tag 不可变，缓存到进程退出）。两个端点共用同一份缓存。
- **不经过 gh-proxy 拉清单**。如果清单和 zip 都从同一个代理拿，代理被篡改时可以同时改 zip 和清单，校验就失去意义。直连 GitHub 是独立的信任锚。
- 拉不到（网络不通、tag 没有清单、解析失败）：返回空 map，客户端按“未校验”处理，仍然可以装；不能因为清单拿不到就把升级卡死。
- **复用桌面构建时**：清单里的文件名是**当前 tag**，哈希必须来自当前 tag 实际复用的那个 zip；不能去 `SOURCE_TAG` 的旧 Release 复制清单，否则文件名对不上，`checksums` 会是空的。
- 如果最终选了国内对象存储，并且把它当作第一方可信源，可以从对象存储拉清单；这时信任锚是对象存储本身。前提是**服务端能访问对象存储**（否则清单和候选都拿不到）。

**信任边界说明：**

- SHA256 能防：传输损坏、下载被截断、镜像返回了错误的文件。
- SHA256 不能防：攻击者同时控制 zip 和清单来源。要防这个，需要发布签名（用私钥签清单、客户端内置公钥），不在本次范围。
- 服务端本身是可信的（客户端已经在信任它给的版本和地址）；清单从服务端下发，等于服务端替客户端做了“去镜像取指纹”的动作。

### 6.5 客户端校验

- 从 URL 的最后一段取文件名，在 `checksums` 里查。
- 有：下载完先算 SHA256，匹配才解压；不匹配就换下一个候选地址；所有候选都不匹配就报错，不装。
- 没有：记一条日志“unverified”，继续走 https 下载（兼容旧服务端和 dev 构建）。
- 融合版用 `fused_checksums`，逻辑相同。

### 6.6 兼容性

- 旧服务端没有 `checksums`：新客户端跳过校验，行为与现在一致。
- 旧客户端不认识 `checksums`：忽略，行为与现在一致。
- 不需要升协议号。

---

## 7. 兼容矩阵

| # | 服务端 | 客户端 | 结果 |
|---|---|---|---|
| A | 新（无 `payloads`，有 `checksums`） | 新（只全量） | 全量下载 + SHA256 校验；正常 |
| B | 新 | 旧（先试载荷） | `payloads` 缺失 → 空 → 全量下载；旧客户端不认 `checksums`，仍能装；正常，只是每次 150MB |
| C | 旧（有 `payloads`，无 `checksums`） | 新 | 忽略 `payloads`；没有 `checksums` → 跳过校验；全量下载；正常 |
| D | 旧 | 旧 | 保持旧行为（载荷优先）；正常 |
| E | 新 | Windows 融合版 | 只认 `fused_downloads` + `fused_checksums`；正常 |
| F | 新 | Android | 不受影响。APK 是**内嵌在服务端二进制里**的（`internal/handler/apk.go:25-49` 的 `fs.ReadFile`），从当前服务器读出来，不是去某个源下载 |
| G | 新 | CLI 自升级 | 协议不变；只是 npm 源顺序变成“用户源优先”，正常 |
| H | 新 | 旧客户端，且当前版本是**载荷装出来的** | 走全量下载；旧的 `app-<v>/` 目录不清理。Windows 每装一个版本会多一份 ~279MB（`install.ts:1002-1003` 的既有行为），列为已知遗留，不是这次引入的 |
| I | 新 | 旧客户端，当前版本是**全量装出来的** | 同 B：全量下载；正常 |
| J | 新 | 新客户端 + **桌面构建复用** | `clientVersion` 指向实际复用的版本；`checksums` 的文件名用当前 tag，哈希来自当前 tag 实际复用的 zip；正常 |
| K | **dev/未打 tag** | 新 | `tag == ""` → 不提供下载、不提供校验和；正常（和现在一致） |

补充：

- **协议号不变**：这次只删字段、加可选字段，没有改客户端/服务端的通信契约，不需要 `ClientProtocol + 1`。
- **旧客户端 + 旧服务端（D）只存在于历史版本**：新发布的 Release 不再有载荷，旧客户端连新服务端就是 B/H，不是 D。
- **载荷装出来的旧目录**：不会迁移、不会删除；新版本侧装到新的 `app-<version>/`，旧目录保留。这是现有行为（Windows 会累积），不是这次新增的。
- **强制升级**：不引入。旧客户端最多是“多下 150MB”，不会打不开。

---

## 8. 待定项：gh-proxy vs 对象存储

### 8.1 背景

删掉载荷后，桌面每次升级都是约 150MB。国内这一份从哪下，直接决定升级成功率和等待时间。

npm 不是选项：文档已记录，历史上 110MB 的桌面 tarball 在 npmmirror 返回 404，49MB 的服务端二进制正常。全量包不能走 npm。

### 8.2 两个选项

| | GitHub + gh-proxy | 国内对象存储/CDN |
|---|---|---|
| 新增基础设施 | 无 | 需要 bucket、域名、上传步骤；国内域名可能涉及备案 |
| 稳定性 | 社区代理，无 SLA；实测 gh-proxy.com 3.9MB/s、ghfast.top 1.2MB/s、github.com 直连 6KB/s（2026-11-11，5MB 区间） | 可控，有 SLA；150MB 的下载时间可预期 |
| 成本 | 几乎为零 | 存储 + 流量费用；按 6 个 zip × 150MB ≈ 900MB/版本粗算，存储约 0.09 元/月量级（0.1 元/GB·月），每次全量升级流量约 0.45 元/次量级（0.5 元/GB）；用你们实际单价替换 |
| 故障表现 | 代理挂了就回落 github.com，可能几小时 | 对象存储故障概率低；真挂了可以切回 GitHub |
| 服务端访问 | 服务端要能直连 GitHub 拉 `SHA256SUMS`；境内机器可能拉不到，拉不到就不校验 | 服务端必须能访问对象存储；能访问时清单和候选都可控 |
| 校验和 | 清单从 GitHub 直连拉；拉不到就不校验 | 清单从对象存储拉；对象存储是第一方，可信任 |
| 对代码的影响 | 现有 `source.ReleaseURLs()` 不变 | 在 `source` 里加一个“第一方镜像”候选，放在国内顺序最前；客户端不用改 |

### 8.3 决策标准

建议按这四个问题定：

1. 桌面用户里国内占多少？如果绝大多数在国内，gh-proxy 的稳定性就是主要风险。
2. 升级频率多高？每次 150MB，频繁升级会把代理的压力和失败率放大。
3. 能不能接受“升级失败后让用户手动去 GitHub 下”？如果不能，选对象存储。
4. 能接受每月多少流量成本？按“版本数 × 平台数 × 150MB × 升级次数”估算，再乘你们对象存储的流量单价。

### 8.4 建议

- **用户量小、升级不频繁**：先用 gh-proxy + SHA256。零基础设施，够用。
- **用户量上来、或者已经出现“升级卡住/失败”的反馈**：换国内对象存储，把它放在国内候选的第一位。
- 两种方案都落在 `source` 的同一个候选列表里，客户端不需要改；区别只在发布流程多一个上传步骤、服务端多一个候选。
- 选对象存储时，发布流程要同时上传 zip 和 `SHA256SUMS`，并定一个归档/过期策略（保留最近 N 个版本）；服务端从对象存储拉清单，客户端仍然只从服务端拿清单和地址。

---

## 9. 测试与验收

### 9.1 源策略包

- 顺序：国内/海外分别断言 `Npm()`、`ReleaseURLs()` 的顺序；Node 顺序仍由 `platform` 的测试覆盖。
- 用户源：环境变量、`npmrc`、两者都有、非法值、http 非回环、重复值。
- URL 拼接：带路径前缀的私有源、结尾斜杠、GitHub 前缀代理。
- 签名常量：`NpmJsBase` / `NpmMirrorBase` 的值固定；`service` 的 `isOfficialRegistry` 仍只认这两个。
- `Region()` 用可替换的 `regionProbe`，测试固定 true/false 两组，不依赖外网。
- CLI 自升级：用户源 401 时记日志并继续地区镜像，仍能升级；用户源返回的 `dist.tarball` 指向 npmjs 时，改写到地区公开镜像，不改写到用户 base。
- 覆盖率：新包自己的测试要覆盖所有导出函数；合并前跑
  `go test ./internal/platform/source/ -coverprofile=/tmp/source.cover` 并贴出数字。不依赖 `service` 的测试。

### 9.2 智能体安装

- 用户源有包：选用户源，只装一次。
- 用户源 404：落到地区镜像。
- 用户源 401/403：不绕过，用用户源装，错误可见。
- 所有源都探不到：用第一个候选装，错误可见。
- 命令已有 `--registry`：不改。
- `curl | bash`：不改；**展示命令与原文完全一致**。
- 本地路径/tarball：不探测。
- Windows 下包名提取走精确切词，不拼 shell 字符串。
- 展示命令和实际执行命令的差异：结果里带实际命令。

### 9.3 桌面

- 只下全量包：没有载荷代码路径。
- SHA256：匹配通过；不匹配换下一个候选；全不匹配报错；没有校验和时跳过并记日志。
- 旧服务端响应（无 `checksums`、有 `payloads`）：能解析、能装。
- 融合版：`fused_checksums` 生效。
- 下载看门狗：慢源会被换掉。

### 9.4 发布

- Release 资产里没有 `-payload`。
- `npm/desktop-payloads` 不再发布；`sync-npmmirror` 的参数里没有 `-payload` 包。
- `SHA256SUMS` 存在，且每个资产的哈希与文件一致。
- 汇总 job 自检：Release 里每个桌面 zip 都在清单里，清单里每一行都有对应资产。
- 复用桌面构建时，校验和与复用的 zip 一致；服务端返回的 `checksums[当前 tag 的文件名]` 非空。
- 融合包的校验和在拼装后生成。
- `openapi.yaml` 里没有 `payloads`，有 `checksums` / `fused_checksums`。

### 9.5 手工验收

- Linux/Windows 各做一次真实升级。
- 把一个镜像地址改坏，确认会换下一个。
- 用一个旧服务端（有 `payloads`、无 `checksums`）配新客户端，确认能装。造法：`git checkout <删载荷前的 commit>` 起服务端，或者把 `/api/desktop/latest` mock 成旧响应。
- 用一个旧客户端配新服务端，确认走全量。
- 把一个候选地址的内容改坏（哈希不匹配），确认换下一个；全坏时报错、不装。
- 国内网络下确认 gh-proxy 顺序生效。
- 用一个装过载荷的旧安装目录，确认新版本侧装到新目录、旧目录不报错。

---

## 10. 风险与取舍

| 风险 | 影响 | 处理 |
|---|---|---|
| 每次升级 150MB | 流量、等待时间 | 用户已明确接受；用镜像和 SHA256 降低失败率 |
| 删掉载荷后失去 npm integrity | 全量包只剩 https | 补 SHA256；清单从 GitHub 直连拉 |
| gh-proxy 不稳定 | 国内升级失败/很慢 | §8 的对象存储选项；候选列表可扩展 |
| 用户源优先可能更慢/不可用 | 智能体安装变慢 | 探测 3 秒超时；404/网络错误自动落到地区镜像；401/403 不绕过 |
| 用户源只代理元数据、不代理 tarball | CLI 升级下载 404 | 把 npmjs 的 tarball 改写到地区公开镜像，绝不改写到用户 base |
| 用户源要求 tarball 鉴权 | CLI 自升级下载失败 | 服务端不复用 npm 的 `_authToken`；明确报错，提示改用公开镜像或手动升级，不静默绕过 |
| CI 里 `SHA256SUMS` 算不出来 | 发布缺校验，客户端静默不校验 | 每个 job 出片段 + 汇总 job 追加；复用路径也重算 |
| 复用路径漏算校验和 | 该平台 `checksums` 为空 | 复用 job 内重新计算并上传片段；汇总 job 自检 |
| 探测增加 3–9 秒 | 安装前多等一会 | 只在安装时探测，列表页不探测；结果里写清选的源 |
| 删载荷涉及面大 | 漏删导致编译/测试失败 | §5.2 清单逐项核对；反向守护“不得出现 payload” |
| 服务端拉不到 `SHA256SUMS` | 本次升级未校验 | 不阻塞，记日志；对象存储可解决 |
| 私有 registry 的 npm 签名不可验证 | 用户源优先后这种警告会更常见 | 与现在一致：警告 + 用户确认，不硬失败；用户想避免就用公开镜像 |

---

## 11. 实施顺序（只列步骤，本文不写代码）

1. **源策略包**：新增 `internal/platform/source` + 测试；把常量、顺序、URL 校验集中进去。**先按 gh-proxy 实现**；对象存储只预留一个候选位置，不阻塞这一步。
2. **服务端迁移**：`upgrade.go`、`upgrade_signature.go`、`desktop_upgrade.go`、`node_runtime.go` 改用 `source`；删重复常量。
3. **智能体安装**：拆 `displayInstallCmd` / `resolveInstallCmd`；接 `source.Npm()`；补探测和 401/404 分支测试。
4. **桌面删载荷**：按 §5.2 从 Go → Electron → CI → npm → 文档逐层删；每层跑对应测试。
5. **SHA256**：CI 生成 `SHA256SUMS`；服务端拉取/缓存/下发；客户端校验；融合版接 `fused_checksums`。
6. **兼容回归**：按 §7 的矩阵手工/单测各走一遍。
7. **托管决策**：按 §8 定 gh-proxy 还是对象存储；如果是对象存储，加一个候选和上传步骤（客户端不变）。

---

## 12. 评审记录

评审人：按 Superpowers `code-reviewer` 评审人设执行（2026-11-19）。

结论：**骨架通过，不能直接实施**；必须先修 4 条 Critical。评审后已按下面处理：

| 评审项 | 结论 | 处理 |
|---|---|---|
| C1 用户源优先会打坏 CLI 自升级 | 部分接受 | **保留用户源优先**（这是需求）；修根因：tarball 只改写到公开镜像、不改写到用户 base；CLI 自升级 401 回落下一个候选，智能体安装 401 不绕过 |
| C2 显示/执行规则矛盾，`curl \| bash` 会被误改 | 接受 | §4.1 明确“只对 `npm install` 开头追加”；§4.2 补 Windows 精确切词、`curl` 原样 |
| C3 CI 里 `SHA256SUMS` 算不出来 | 接受 | §6.2 改成“每个 job 出片段 + 汇总 job `gh release upload`”；复用路径也重算；融合包在拼装后算 |
| C4 删除清单漏 `desktop_test.go` / `openapi.yaml` / 文档 / sync 参数 | 接受 | §5.2 已补全，并加了删除后的 grep 自检 |
| C5 兼容矩阵漏“装过载荷的旧客户端”等 | 接受 | §7 增加 H/I/J/K 行和 Windows 目录累积说明 |
| I1 `source` 接口不完整 | 接受 | 改成 `ReleaseURLs(repo, tag, asset)`，repo 常量搬进 `source` |
| I2 新包覆盖率/外网依赖 | 接受 | `Region()` 用可替换的 `regionProbe`；§9.1 加覆盖率命令 |
| I3 签名语义不该进 `source` | 接受 | `source` 只导出两个 base 常量，`isOfficialRegistry` 留在 `service` |
| I4 “下载不重试”原则和现状打架 | 接受 | 改成“能拿到多个候选 URL 的下载逐个试；CLI 升级当前只下一个，本次不改” |
| I5 探测位置/成功口径 | 接受 | §4.2 写明在安装机器上跑、成功 = 退出码 0 且输出版本号 |
| I6 服务端视角 vs 客户端视角 | 接受 | §4.1 补一句：智能体安装读服务端机器配置，和桌面升级刻意相反 |
| I7 对象存储的访问/成本/清单 | 接受 | §8 增加“服务端必须能访问”、成本量级、清单上传和归档策略 |
| I8 漏 `labels.ts` | 接受 | §5.2 Electron 表补 `labels.ts` |
| I9 复用路径 `checksums` 为空 | 接受 | §9.4 增加断言 |
| M1–M7 | 采纳 | http 私有源行为变化、`Candidate` 判据、macOS 说明、旧服务端造法、实施顺序、平台表、评审回填 |

**评审人提出但未采纳的一条**：C1 建议把 CLI 自升级改回“地区镜像优先”。不采纳，因为和本次明确的需求“智能体与后端包同一套、用户源优先”冲突；改用“保留用户源优先 + 修 tarball 改写 + 分链路 401”来解决根因。

评审人给出的实施前置条件：C1–C4 修完即可进入实施；I1 建议在设计阶段就定（已定）。

### 12.1 实现差异

- **Node 顺序留在 `platform`，没有进 `source`**。`source` 要 import `platform` 取地区判断和用户目录；如果 `platform/node_runtime.go` 再 import `source`，就成环。Node 本来就只有一份实现，保持原样不算“各写一套”。
- **`desktopReleaseRepo` 留在 `service`**。`source.ReleaseURLs(repo, tag, asset)` 只负责拼完整 URL 和前缀顺序；仓库名属于产品配置，放在 `service` 更合适。
- **签名信任锚没有搬进 `source`**。`source` 只导出 `NpmJsBase` / `NpmMirrorBase` 两个常量，`service` 的 `isOfficialRegistry` 继续判断“这个源该不该带 npm 签名”。
- **CLI 升级的候选预算从 3s×2 调整为 2s×3**：候选从两个变成三个（用户、地区、另一个公开源），总预算 3×2s + 2s 签名 = 8s，仍在 9s 检查上限和前端 10s 之内。
- **删掉了 `desktop/src/shared/integrity.ts`**：它只服务载荷包的 SRI 校验，载荷删除后没有生产调用方。
- **`scripts/lib/reuse-client-artifact.sh` 的 desktop 分支从 5 个参数降到 3 个**：`<asset_base> <source_tag> <dest_zip>`。
- **CI 用 `desktop-*-zip` artifact 汇总 SHA256**：`action-gh-release` 直接上传的文件没有本地副本，所以每个桌面 job 额外上传 zip artifact，`desktop-checksums` job 下载后 `sha256sum` 并 `gh release upload`。

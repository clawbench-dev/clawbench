# 上游基线记录

2026-11-12 从 GitHub 下载按 commit 锁定的 ZIP，覆盖镜像到 `clawbench-source code/`，逐文件校验 4,420 个文件，没有创建旧版备份。相对 `21aeaff4` 共新增 81 个提交；同时核对 v0.138.0、v0.139.0 更新记录、Android WebView 生命周期修复及共享原生桥。参考源码保持原始内容；本轮共享前端修复单独归档为最小补丁，并在隔离贡献检出中实施。

## 当前快照

| 字段 | 当前值 |
| --- | --- |
| 上游仓库 | `https://github.com/clawbench-dev/clawbench` |
| 上游分支 / commit | `main` / `733ab3ec0c039193eca80dd08816f2e9006aa0aa` |
| 发布 tag | `v0.139.0`，tag 指向同一 commit |
| 同步日期 | `2026-11-12` |
| 本地参考目录 | `clawbench-source code/`，不纳入发布源码 |
| 归档 SHA-256 | `93bbe3d1e8672615ce5fb679ed8f696893a4f69a6626a20b42ea837aedd753ca` |
| 上游文件数 | 4,420 |
| 客户端协议号 | `1`，来自 `internal/version/client_protocol.txt` |
| 鸿蒙工程版本 | `0.2.12 / 212`，包名 `com.clawbench.harmony` |
| HarmonyOS 集成状态 | PR #631、#636、#653、#662 已合并；本地开工时的邮件评审修改与当前上游运行时代码一致，不重复提交 |
| 本轮共享修复 | `web/src/components/attach/AttachmentPicker.vue` 与对应回归测试；维护方另存补丁归档 |
| release 资产 | 最新正式 release 没有 HarmonyOS HAP，应用内升级来源仍待上游发布 |

共享前端与服务端单独部署，HAP 不内嵌整套 Vue 应用。附件宿主判定修复须由服务端更新前端后生效，单独安装 HAP 不会替换服务端页面。原始参考目录、账户配置、签名材料和本地指令文件不进入贡献改动。

## 原始适配基线

以下记录描述 2026-11-06 首次创建本地宿主补丁时使用的基线，不是当前参考源码版本。

| 字段 | 原始值 |
| --- | --- |
| commit | `252b5289424fcf86cf935b722e09fa48d8ecee32` |
| 归档 SHA-256 | `c810792c71301ccb0cd995b003a8c101b16c92efc428717d9d61c43809ff37f9` |
| 文件数 | 4,151 |
| 客户端协议号 | `1` |
| 补丁 | 首次适配所用的历史本地补丁涉及 30 个路径；本仓库不发布该维护材料 |
| 当时验证 | 418 项定向测试、隧道设置 81 项、vue-tsc 和 Vite 生产构建通过 |

上游许可证由 `LICENSE` 标明为 MIT。鸿蒙端复用上游 logo、安卓登录页和地址工具，来源及许可记录见 `ANDROID_LOGIN_ASSETS.md` 与 `docs/licenses/ClawBench-MIT.txt`。旧基线补丁作为历史移植记录保留；不代表当前上游仍需要该补丁。

## 主要文件指纹

以下 SHA-256 用于识别 2026-11-12 当前快照中的关键文件，不能替代完整 commit，也不能单独证明整个目录来自同一版本。

```text
android/app/src/main/java/com/clawbench/app/MainActivity.java
dfeb5f4e58fa6cc2e877dfe85ecf303cbd078c5a4c6f765a3aab51d37ad4ddee

android/app/src/main/AndroidManifest.xml
f319f13b410230fb7444e5b63d29dff0278840c80bba058b5edaf7b8a780a210

web/src/utils/clawbenchNative.ts
0d3e3b18030520f24b71c4eb9fc4d773052b19c9572b6a8443badadfeea4585e

web/src/composables/usePlatformDetect.ts
ab761449597b728d5277e0830b48e03cc23d0a324f678d8b5b375f226eeb4fdf

internal/version/client_protocol.txt
4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865
```

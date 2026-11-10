# 上游基线记录

当前参考源码已于 2026-11-10 从 GitHub 下载并覆盖同步到上游 `main` / 正式 tag `v0.137.0`。相对上轮 `06a43010` 新增 45 个提交；主线和 tag 指向同一 commit。ZIP 经过路径、归档 SHA-256 和镜像后逐文件内容校验，参考目录没有创建旧版本备份。上游 `CLAUDE.md` 是指向 `AGENTS.md` 的归档符号链接；Windows ZIP 解压保留为普通文本文件，不创建文件系统链接。鸿蒙运行时改动独立审核、构建，不把服务器源码整体打入 HAP。

## 当前快照

| 字段 | 当前值 |
| --- | --- |
| 上游仓库 | `https://github.com/clawbench-dev/clawbench` |
| 上游分支 / commit | `main` / `21aeaff4c6ef066aa94ad50d884518b01b4744de` |
| 发布 tag | `v0.137.0`，tag 指向同一 commit |
| 同步日期 | `2026-11-10` |
| 本地参考目录 | `clawbench-source code/` |
| 归档 SHA-256 | `6d59d993249137c45bec992fde15072c25d5868aacc05b701ed87ab36c5af91a` |
| 上游文件数 | 4,380 |
| 客户端协议号 | `1`，来自 `internal/version/client_protocol.txt` |
| 鸿蒙工程版本 | 本轮更新为 `0.2.11 / 211`，包名 `com.clawbench.harmony` |
| HarmonyOS 集成状态 | PR #631 / #636 的集成及后续 0.2.10 修复已进入上游；此快照已包含鸿蒙宿主，不要再叠加历史补丁 |
| 鸿蒙 release 资产 | `v0.137.0` release 只有 Android、桌面和服务端资产，没有 HarmonyOS HAP；应用内更新检测待上游发布鸿蒙 release 后再验收 |
| 发布状态 | README 标注实验性支持，明确未达到 Android 全功能对等且暂不进入正式发布流水线 |

历史 Harmony 集成补丁与校验材料属于维护方本地工作流，不随本仓库提交。上游主线已包含鸿蒙宿主时，无需也不应重放历史补丁。

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

以下 SHA-256 用于识别 2026-11-10 当前快照中的关键文件，不能替代完整 commit，也不能单独证明整个目录来自同一版本。

```text
android/app/src/main/java/com/clawbench/app/MainActivity.java
bcd706c830ed96c82e6656d1b0be1c9482cbf1247755452417493856a0669d31

android/app/src/main/AndroidManifest.xml
f319f13b410230fb7444e5b63d29dff0278840c80bba058b5edaf7b8a780a210

web/src/utils/clawbenchNative.ts
0d3e3b18030520f24b71c4eb9fc4d773052b19c9572b6a8443badadfeea4585e

web/src/composables/usePlatformDetect.ts
ab761449597b728d5277e0830b48e03cc23d0a324f678d8b5b375f226eeb4fdf

internal/version/client_protocol.txt
4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865
```

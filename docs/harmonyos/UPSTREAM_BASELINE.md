# 上游基线记录

记录日期：2026-11-06

| 字段 | 当前值 |
| --- | --- |
| 上游仓库 | `https://github.com/clawbench-dev/clawbench` |
| 本地参考目录 | `clawbench-source code/` |
| 本次行为 | 下载锁定提交归档并覆盖参考目录；未创建旧源码备份 |
| 当前快照 commit / tag | `252b5289424fcf86cf935b722e09fa48d8ecee32` / 未据此认定发布 tag |
| 默认分支 | `main`，由 `git ls-remote --symref` 查询 |
| 是否确认最新 | 是，仅指 2026-11-06 同步时的默认分支 HEAD |
| 归档 SHA-256 | `c810792c71301ccb0cd995b003a8c101b16c92efc428717d9d61c43809ff37f9` |
| 清单检查 | 4151 个文件，覆盖后没有额外陈旧文件 |
| 客户端协议号 | `1`，来自 `internal/version/client_protocol.txt` |
| 上游许可证 | 本地 `LICENSE` 标明 MIT |
| 鸿蒙代码复用情况 | 复用上游 logo、安卓登录页和地址工具，详见 `ANDROID_LOGIN_ASSETS.md`；MIT 声明保留在 `docs/licenses/ClawBench-MIT.txt` |
| 适配补丁 | `patches/upstream/0001-harmonyos-host.patch`，26 个文件，含独立宿主、通知、剪贴板、分享和隧道设置回归 |
| 补丁复现检查 | 原始归档 `git apply --check` 通过；已修改目录反向检查通过 |
| 前端验证 | 418 项定向测试与隧道设置 81 项通过，vue-tsc、Vite 生产构建成功；已有异步资源告警见维护记录，未部署共享服务器 |

以下 SHA-256 用于识别本次阅读的关键文件，不能替代完整上游 commit，也不能证明目录内所有文件来自同一版本。

```text
android/app/src/main/java/com/clawbench/app/MainActivity.java
1e3cd429a8ba33f50b3ada7ea9ab76ac6afafb1ffee78577a77e327a8730f10c

android/app/src/main/AndroidManifest.xml
f319f13b410230fb7444e5b63d29dff0278840c80bba058b5edaf7b8a780a210

web/src/utils/clawbenchNative.ts
13bb805df378ecebd4587aed82570d17bc54e10abc77b2a5f972cf7d26ccf625

web/src/composables/usePlatformDetect.ts
cc162cc3eb1f5659d3b5c5a0163343a86122b77b1496b25bb5eb12cdb8e60d38

internal/version/client_protocol.txt
4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865
```

后续覆盖同步更新完整 commit、分支或 tag、下载日期、归档 SHA-256、协议号、补丁清单和验证结果，不创建旧源码备份。以下早期关键文件指纹保留为初始化阅读记录，不替代当前锁定归档。

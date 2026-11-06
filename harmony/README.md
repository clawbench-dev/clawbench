# HarmonyOS 客户端（实验性）

基于 ArkTS / ArkUI Stage 和 ArkWeb，复用安卓本地登录页及服务端 Vue 工作台。当前版本 `0.1.3`，客户端协议 `1`。这是可构建的初始移植，尚未达到 Android 全功能对等，暂不加入正式发布流水线。

## 开发与构建

本轮环境：DevEco Studio 26.0.0.851、SDK 26.0.0.105、Hvigor 6.26.8、ohpm 26.0.0.630。兼容 / 目标 API 23（HarmonyOS 6.1），编译 API 26；API 23 真机未验收。

在仓库根目录构建：

```powershell
./scripts/build-harmonyos.ps1 -DevEcoRoot 'F:\DevEco Studio'
```

也可以用 DevEco Studio 打开 `harmony/`。产物在 `entry/build/default/outputs/default/`，默认未签名。签名配置应由开发者本地提供，不提交证书、私钥、密码、实际服务器地址或账户。

Node.js 24 单元与登录页契约测试：

```sh
npm ci --prefix harmony
npm --prefix harmony test
```

鸿蒙测试独立于根目录 Vitest；jsdom 只用于开发测试，不进入 HAP。前端宿主回归仍使用根目录既有 Vitest、类型检查和构建流程。

## 当前能力

- 本地服务器列表、命名、增删改、协议健康检查与原生 `/login` 认证。
- Preferences 保存非敏感偏好，AssetStore 按归一化服务器地址保存密码。密码不回传给远程页面，本地登录页仅持有临时引用。
- 独立本地 / 远程 ArkWeb 控制器、可信顶层文档白名单、随机消息端口、取消和连接代次过滤。
- 全面屏动态避让状态栏、挖孔、导航指示器；系统栏跟随实际页面背景，键盘使用 RESIZE，登录页允许滚动。
- 返回事件、前后台恢复 hook、常亮释放、外链和系统文件选择入口。
- 前端独立识别 HarmonyOS，不启用 Android WebView CSS 补偿或 APK 更新，不报告尚未实现的隧道、推送、录屏等能力。

## 验证与边界

25 项鸿蒙 Node 测试通过，未签名 HAP 构建成功；在 Pura 90 Pro / HarmonyOS 7 / API 26 / x86_64 模拟器验证安装、已保存凭据登录、服务器管理、竖屏安全区和聊天键盘布局。模拟器旋转检查时连接丢失，横屏设备验收待补。

尚未验收 ARM64 / API 23 真机、同版本 Android 逐像素对照、平板 / 分屏 / 大字体、全主题、长时后台，以及完整业务回归。通知、原生下载分享、SSH / HTTP2 隧道、摄像头 / 麦克风 / 录屏、悬浮状态、证书信任及自动更新仍在移植范围内。系统文件选择已接入口，未完成端到端设备验收。

完整范围见 [功能对齐清单](../docs/harmonyos/FEATURE_PARITY.md)。登录资源和 MIT 许可来源见 [安卓登录页复用记录](../docs/harmonyos/ANDROID_LOGIN_ASSETS.md)。

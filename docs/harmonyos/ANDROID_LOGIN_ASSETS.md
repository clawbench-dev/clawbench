# 安卓登录页复用记录

版本：0.1.2。来源为锁定上游 `clawbench-dev/clawbench` 提交 `252b5289424fcf86cf935b722e09fa48d8ecee32`，不是另做一套相似页面。

安卓 `MainActivity.java` 使用 `file:///android_asset/login.html` 打开本地登录页，工作台则由 Android WebView 加载服务端 Vue。鸿蒙采用相同的本地登录 / 远程工作台划分，Java 系统层独立移植为 ArkTS。

## 资源与许可

| 上游路径 | 鸿蒙打包路径，相对于 `harmony/entry/src/main/resources/rawfile/` | 原始 SHA-256 |
| --- | --- | --- |
| `android/app/src/main/assets/login.html` | `login/index.html` | `5d7f848171aeef259f5282377f16636594e919ad77b73638f406333c6649174b` |
| `android/app/src/main/assets/url-utils.js` | `login/url-utils.js` | `54de83ac80f7a4da35bfb93f13c642ce3ededd0bebb05473bc139c4da43b83ca` |
| `android/app/src/main/assets/logo.png` | `login/logo.png` | `0fdf12d84752af5c70dac9cf9006f0dfb65a155a38b404d9396e89f5a2f4dfd1` |

上游 MIT 声明保留于 [ClawBench-MIT.txt](licenses/ClawBench-MIT.txt)。此处仅复用实际运行需要的三个资源，整个参考源码目录继续忽略。

## 鸿蒙适配范围

- 保留安卓页面样式、布局、主题、双语文案、图标、列表、添加 / 编辑表单及删除确认。没有重新设计登录页。
- 增加 CSP 和本地 `login-bootstrap.js`，不访问网络、不加载 frame、不允许表单导航。
- 通过独立本地控制器的只读 `ClawBenchLoginInit.getConfig()` 在脚本执行前取得外观、版本和服务器快照。不能用仅适用于 HTTP/HTTPS 的 document-start 注入来初始化资源文档。
- `ClawBenchNative` 仅存在于打包的本地登录页。同步 getter 返回缓存，保存、删除、外观持久化与连接通过随机通道消息端口等待原生结果。
- AssetStore 中的密码不回传；快照的 password 字段是本次登录会话的随机引用。连接及编辑保留密码由原生解析，离开登录页即清理引用。
- 连接成功后才保存新认证密码；失败使用安卓 `onConnectError` 和分类码显示原有错误状态。异步存储失败保留表单，改地址先保存再删除旧地址。
- 地址比较与原生归一化一致，防止显式默认端口、大小写变化误判为改址或覆盖另一条凭据；已有省略默认端口的地址可直接编辑。
- 系统返回优先关闭确认框、返回服务器列表；连接中返回取消本次连接。
- 0.1.3 增加纵向滚动及页面内边距以适配短屏 / 键盘，加载系统栏配色脚本；整体主题、品牌与表单布局继续沿用安卓。

远程工作台使用另一个 Web 控制器，不注册登录初始化代理，也不暴露此处的密码引用或 `ClawBenchNative` 别名；仍由独立 `ClawBenchHarmony` 宿主补丁适配。

## 后续同步

按根目录维护要求覆盖更新忽略的上游参考目录后，记录新的完整 commit，并核对三个资源及 Java 桥契约。对比当前登录 HTML 与新基线，保留上述必要适配，不自动覆盖丢失改动；`url-utils.js`、logo 当前未修改。

执行根目录 `npm ci --prefix harmony`、`npm --prefix harmony test` 和 `./scripts/build-harmonyos.ps1`，再进行鸿蒙设备检查。至少检查空列表、已保存凭据、错误认证、新增 / 编辑 / 删除、默认端口、取消 / 返回、主题 / 语言与工作台无额外顶栏。记录构建指纹、设备范围，更新维护文档并本地提交。

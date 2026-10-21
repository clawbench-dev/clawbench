# @xulongzhe/clawbench-android-arm64

Platform binary for [ClawBench](https://github.com/clawbench-dev/clawbench) running inside [Termux](https://github.com/clawbench-dev/clawbench/blob/main/docs/user-guide/TERMUX.md) on Android (arm64, PIE).

**Do not install this package directly.** Install the launcher instead:

```bash
pkg install -y nodejs-lts git
npm install -g @xulongzhe/clawbench
clawbench
```

It pulls the binary package matching your platform via `optionalDependencies` and exposes the `clawbench` command.

- 项目主页 / 源码：<https://github.com/clawbench-dev/clawbench>
- Termux 指南：<https://github.com/clawbench-dev/clawbench/blob/main/docs/user-guide/TERMUX.md>
- 许可证：[MIT](https://github.com/clawbench-dev/clawbench/blob/main/LICENSE)

#!/usr/bin/env node
import { spawn } from "child_process";
import { resolve, dirname } from "path";
import { fileURLToPath } from "url";
import { createRequire } from "module";
import os from "os";

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);

const PLATFORM_MAP = {
  "linux-x64": "@xulongzhe/clawbench-linux-x64",
  "linux-arm64": "@xulongzhe/clawbench-linux-arm64",
  "darwin-x64": "@xulongzhe/clawbench-darwin-x64",
  "darwin-arm64": "@xulongzhe/clawbench-darwin-arm64",
  "win32-x64": "@xulongzhe/clawbench-win32-x64",
};

const key = `${process.platform}-${process.arch}`;
const pkg = PLATFORM_MAP[key];

if (!pkg) {
  console.error(`clawbench: 不支持的平台 ${key}`);
  process.exit(1);
}

const binName = process.platform === "win32" ? "clawbench.exe" : "clawbench";

let binPath;
if (process.env.CLAWBENCH_BINARY_PATH) {
  binPath = process.env.CLAWBENCH_BINARY_PATH;
} else {
  try {
    const platformPkgPath = require.resolve(`${pkg}/package.json`);
    const platformDir = dirname(platformPkgPath);
    binPath = resolve(platformDir, "bin", binName);
  } catch {
    console.error(`clawbench: 平台包 "${pkg}" 未安装`);
    console.error(`  请运行: npm install ${pkg}`);
    console.error(`  或设置环境变量: CLAWBENCH_BINARY_PATH=/path/to/clawbench`);
    process.exit(1);
  }
}

const child = spawn(binPath, process.argv.slice(2), {
  stdio: "inherit",
  env: { ...process.env },
});

// 转发信号到子进程
process.on("SIGINT", () => child.kill("SIGINT"));
process.on("SIGTERM", () => child.kill("SIGTERM"));

child.on("exit", (code, signal) => {
  if (signal) {
    const sigNum = os.constants.signals[signal] ?? 0;
    process.exit(128 + sigNum);
  }
  process.exit(code ?? 1);
});

child.on("error", (err) => {
  console.error(`clawbench: 启动失败: ${err.message}`);
  process.exit(1);
});

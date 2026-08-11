#!/usr/bin/env node
// postinstall fallback: 当 optionalDependencies 被禁用时，
// 提示用户手动安装平台包。
import { readFileSync } from "fs";
import { dirname, join } from "path";
import { fileURLToPath } from "url";
import { createRequire } from "module";

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);

const PLATFORM_MAP = {
  "linux-x64": "@xulongzhe/clawbench-desktop-linux-x64",
  "linux-arm64": "@xulongzhe/clawbench-desktop-linux-arm64",
  "darwin-x64": "@xulongzhe/clawbench-desktop-darwin-x64",
  "darwin-arm64": "@xulongzhe/clawbench-desktop-darwin-arm64",
  "win32-x64": "@xulongzhe/clawbench-desktop-win32-x64",
};

const key = `${process.platform}-${process.arch}`;
const pkg = PLATFORM_MAP[key];

if (!pkg) {
  // 不支持的平台，静默跳过
  process.exit(0);
}

try {
  require.resolve(`${pkg}/package.json`);
  process.exit(0); // 已安装
} catch {
  // 未安装
}

const mainPkg = JSON.parse(readFileSync(join(__dirname, "package.json"), "utf8"));
const version = mainPkg.version;

console.log(`clawbench-desktop: 平台包未安装，请手动安装: npm install ${pkg}@${version}`);
console.log(`  或设置环境变量: CLAWBENCH_DESKTOP_PATH=/path/to/clawbench-desktop`);

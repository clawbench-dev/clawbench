#!/usr/bin/env node
// postinstall fallback: 当 optionalDependencies 被禁用时，
// 提示用户手动安装平台包。
import { readFileSync } from "fs";
import { dirname, join } from "path";
import { fileURLToPath } from "url";
import { createRequire } from "module";
import { resolvePlatformPackage } from "./bin/platform.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);

const resolved = resolvePlatformPackage(process.platform, process.arch);

if (!resolved) {
  // 不支持的平台，静默跳过
  process.exit(0);
}

const { pkg } = resolved;

// 检查平台包是否已通过 optionalDependencies 安装
try {
  require.resolve(`${pkg}/package.json`);
  process.exit(0); // 已安装
} catch {
  // 未安装
}

// 读取主包版本
const mainPkg = JSON.parse(readFileSync(join(__dirname, "package.json"), "utf8"));
const version = mainPkg.version;

console.log(`clawbench: 平台包未安装，请手动安装: npm install ${pkg}@${version}`);
console.log(`  或设置环境变量: CLAWBENCH_BINARY_PATH=/path/to/clawbench`);

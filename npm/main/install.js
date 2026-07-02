#!/usr/bin/env node
// postinstall fallback: 当 optionalDependencies 被禁用时，
// 提示用户手动安装平台包。
import { dirname, join } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));

const PLATFORM_MAP = {
  "linux-x64": "@xulongzhe/clawbench-linux-x64",
  "linux-arm64": "@xulongzhe/clawbench-linux-arm64",
  "darwin-x64": "@xulongzhe/clawbench-darwin-x64",
  "darwin-arm64": "@xulongzhe/clawbench-darwin-arm64",
  "win32-x64": "@xulongzhe/clawbench-win32-x64",
};

async function install() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORM_MAP[key];

  if (!pkg) {
    console.log(`clawbench: 不支持的平台 ${key}，跳过安装`);
    return;
  }

  // 检查平台包是否已通过 optionalDependencies 安装
  try {
    require.resolve(`${pkg}/package.json`);
    return; // 已安装，无需额外操作
  } catch {
    // 未安装
  }

  // 读取主包版本
  const { readFileSync } = await import("fs");
  const mainPkg = JSON.parse(
    readFileSync(join(__dirname, "package.json"), "utf8")
  );
  const version = mainPkg.version;

  console.log(`clawbench: 平台包未安装，请手动安装: npm install ${pkg}@${version}`);
  console.log(`clawbench: 或设置 CLAWBENCH_BINARY_PATH 环境变量指定二进制路径`);
}

install().catch((err) => {
  console.error(`clawbench: 安装失败: ${err.message}`);
  console.error(`clawbench: 请手动安装对应平台包，或设置 CLAWBENCH_BINARY_PATH`);
});

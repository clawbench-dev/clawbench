// Platform → npm package resolution for the @xulongzhe/clawbench platform
// binaries. Kept as a pure module (no side effects) so it can be unit tested
// independently of the launcher's spawn logic.

export const PLATFORM_MAP = {
  "linux-x64": "@xulongzhe/clawbench-linux-x64",
  "linux-arm64": "@xulongzhe/clawbench-linux-arm64",
  "android-arm64": "@xulongzhe/clawbench-android-arm64",
  "darwin-x64": "@xulongzhe/clawbench-darwin-x64",
  "darwin-arm64": "@xulongzhe/clawbench-darwin-arm64",
  "win32-x64": "@xulongzhe/clawbench-win32-x64",
};

// Termux 上报 process.platform === "android"，它运行在 Android 之上，
// 只接受 PIE（e_type 3 / ET_DYN）的可执行文件。linux-arm64 二进制是
// 非 PIE（e_type 2 / ET_EXEC），无法在 Android 上执行，因此 android
// 必须单独映射到 GOOS=android 编译的 PIE 包，而不是复用 linux。
export function resolvePlatformKey(platform, arch) {
  return `${platform}-${arch}`;
}

// Returns { key, pkg } for a supported platform/arch, or null if unsupported.
export function resolvePlatformPackage(platform, arch) {
  const key = resolvePlatformKey(platform, arch);
  const pkg = PLATFORM_MAP[key];
  if (!pkg) return null;
  return { key, pkg };
}

export function resolveBinName(platform) {
  return platform === "win32" ? "clawbench.exe" : "clawbench";
}

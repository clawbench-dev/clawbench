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

// Returns the spawn options used to launch the server binary.
//
// `detached: true` puts the server in its own session/process group. Without
// it the server inherits the launcher's group, so a terminal hangup (SSH
// disconnect, closing the shell) delivers SIGHUP to the whole group and the
// server dies. `nohup` cannot prevent this: it only sets SIG_IGN on the
// launcher, and Node resets that disposition to SIG_DFL for itself, so the
// signal reaches the binary regardless. The directly-installed binary has no
// launcher layer and survives a hangup, which is why only the npm install
// showed the problem.
//
// Signals are still forwarded explicitly by the launcher, so Ctrl+C and
// `kill <launcher-pid>` keep stopping the server.
export function resolveSpawnOptions(env) {
  return {
    stdio: "inherit",
    env,
    detached: true,
  };
}

import { app, dialog, type BrowserWindow } from 'electron'
import { fetchDesktopLatest, type DesktopLatest } from './updater'
import {
  downloadAndInstall,
  installPayload,
  restartInto,
  appRoot,
} from './install'
import { recordError } from './clientLog'
import { compareVersions, shouldGate } from '../shared/version'

/**
 * The desktop client/server version-consistency gate.
 *
 * The desktop shell ships in lockstep with the server, so a mismatch in EITHER
 * direction means the two may be incompatible. Android has gated this before the
 * WebView loads for a while (`VersionCompare.shouldShowMismatch`); this is the
 * desktop counterpart, surfaced through the native overlay in `splash.ts`.
 *
 * Only DECISIONS live in `evaluateGate` (pure, unit-tested). The rest is
 * Electron/filesystem glue: fetching the server's release info, and installing
 * the server's version when the user asks for it.
 */

export interface VersionGateInfo {
  /** The running desktop version (`app.getVersion()`, bare — no leading v). */
  clientVersion: string
  /** The version the server reports for itself. */
  serverVersion: string
  /**
   * Which side is newer. `older` means the client is behind the server (the
   * classic "upgrade the app" case); `newer` means the client is AHEAD, so
   * "download" aligns it down to the server's version. The overlay picks its
   * wording from this — "update" would be wrong for `newer`.
   */
  direction: 'older' | 'newer'
  /** Candidate URLs for the full package, best-first. */
  urls: string[]
  /** Candidate URLs for the payload-only archive, best-first (empty = full download). */
  payloadUrls: string[]
}

/**
 * Pure gate decision: given the running client version, the server's release
 * info and whether this is a packaged build, decide whether to block.
 *
 * Returns null when there is nothing to block on. `fetchDesktopLatest` already
 * returns null for a dev/untagged server, so `latest` here is always a real
 * release with at least one download URL.
 */
export function evaluateGate(
  clientVersion: string,
  latest: DesktopLatest,
  packaged: boolean,
): VersionGateInfo | null {
  if (!shouldGate(clientVersion, latest.version, { packaged })) return null
  return {
    clientVersion,
    serverVersion: latest.version,
    direction: compareVersions(clientVersion, latest.version) < 0 ? 'older' : 'newer',
    urls: latest.urls,
    payloadUrls: latest.payloadUrls,
  }
}

/**
 * Navigation generation.
 *
 * The version check is asynchronous and can resolve after the user has already
 * cancelled or switched servers. Each navigation bumps the generation, and a
 * result whose generation is stale is dropped — otherwise a check for an
 * abandoned connection could raise the blocking overlay over the login page.
 */
let generation = 0

/** Bump the navigation generation, invalidating any in-flight gate check. */
export function beginNavigation(): number {
  generation += 1
  return generation
}

/** The gate currently shown, or null. Consumed by the download action. */
let activeGate: VersionGateInfo | null = null

export function setActiveGate(info: VersionGateInfo | null): void {
  activeGate = info
}

export function getActiveGate(): VersionGateInfo | null {
  return activeGate
}

/**
 * Fetch the server's release info and decide whether to gate.
 *
 * Returns null when the generation is stale (a newer navigation started), the
 * URL is not a remote server, the server exposes no release, or the versions
 * agree. Never throws — a gate check must not be able to break a connection.
 */
export async function checkVersionGate(url: string, gen: number): Promise<VersionGateInfo | null> {
  // A local document (the first-run login page) has no server version to check.
  if (!/^https?:\/\//i.test(url)) return null
  if (gen !== generation) return null

  let latest: DesktopLatest | null
  try {
    latest = await fetchDesktopLatest(url)
  } catch {
    return null
  }
  if (!latest) return null
  // The navigation may have moved on while the request was in flight.
  if (gen !== generation) return null

  return evaluateGate(app.getVersion(), latest, app.isPackaged)
}

/**
 * Build the `detail` text for the "install failed" dialog.
 *
 * The payload (npm) path is attempted FIRST and falls back to the full package.
 * It used to log its failure to console only, so the dialog showed just the
 * github.com full-package error — which reads as "the client never tried npm",
 * when in fact npm was tried and failed. On a machine where github.com is
 * unreachable (mainland China) that is the whole story, and hiding the npm
 * error sends the reader down the wrong path entirely.
 *
 * When the payload was attempted, BOTH errors are shown, each labelled with the
 * source it came from.
 */
export function formatInstallFailure(fullErr: unknown, payloadErr: unknown): string {
  const full = String((fullErr as Error)?.message || fullErr)
  if (payloadErr == null) return full
  const payload = String((payloadErr as Error)?.message || payloadErr)
  return `增量包（npm）安装失败：\n${payload}\n\n全量包下载失败：\n${full}`
}

/**
 * Install the server's version (the user chose "download" on the gate).
 *
 * Prefers the small payload archive and falls back to the full package, exactly
 * as the startup upgrade used to. On failure the gate STAYS UP with a native
 * error dialog, so the user is not dropped into an app known to be mismatched.
 *
 * A client NEWER than the server installs the server's older build — that is the
 * point (align to the server), and `install.ts` accepts a downgrade.
 *
 * Returns true when the requested version is now installed (whether or not the
 * process has restarted into it yet), false when nothing was installed.
 */
export async function installServerVersion(
  info: VersionGateInfo,
  parent: BrowserWindow | null,
): Promise<boolean> {
  const parentArg = parent ?? undefined

  // Prefer the small payload archive: it carries only our app code (~3MB) and
  // reuses the installed Electron runtime, instead of re-downloading ~150MB.
  //
  // macOS is excluded because replacing resources/app.asar inside a signed
  // .app breaks the code-signature seal (Apple Silicon requires a valid one),
  // and the server publishes no payload for it either. Any payload failure —
  // a dead mirror, an ABI mismatch, a malformed archive — falls through to the
  // full download below rather than failing the upgrade.
  let installed = false
  let payloadErr: unknown = null
  if (process.platform !== 'darwin' && info.payloadUrls.length > 0) {
    try {
      await installPayload(info.payloadUrls, info.serverVersion, appRoot())
      installed = true
    } catch (err) {
      console.error('[gate] payload install failed, falling back to full download:', err)
      recordError('Install', err)
      // Keep it so the failure dialog can show BOTH attempts — otherwise the
      // user only ever sees the github.com error and concludes npm was skipped.
      payloadErr = err
    }
  }

  try {
    if (!installed) await downloadAndInstall(info.urls, info.serverVersion, '')
  } catch (err) {
    await dialog.showMessageBox(parentArg as BrowserWindow, {
      type: 'error',
      buttons: ['确定'],
      title: '下载失败',
      message: '下载或安装该版本失败，当前版本不受影响。',
      detail: formatInstallFailure(err, payloadErr),
    })
    return false
  }

  const { response } = await dialog.showMessageBox(parentArg as BrowserWindow, {
    type: 'info',
    buttons: ['立即重启', '稍后'],
    defaultId: 0,
    cancelId: 1,
    title: '版本已就绪',
    message: `ClawBench 桌面版 ${info.serverVersion} 已安装。`,
    // The pointer is already flipped, and startup reads it itself, so "later"
    // still lands on the new version on the next launch.
    detail: '重启后生效。选择“稍后”也可在下次启动时自动使用该版本。',
  })
  // The install SUCCEEDED, so report it even when the user defers the restart:
  // the caller closes the gate, which would otherwise keep offering a download
  // of the version that is already on disk.
  if (response !== 0) return true

  try {
    restartInto(info.serverVersion)
  } catch (err) {
    await dialog.showMessageBox(parentArg as BrowserWindow, {
      type: 'error',
      buttons: ['确定'],
      title: '重启失败',
      message: '无法启动该版本，请手动重启应用。',
      detail: String((err as Error)?.message || err),
    })
  }
  // The version is installed either way; the restart is just how it takes effect.
  return true
}

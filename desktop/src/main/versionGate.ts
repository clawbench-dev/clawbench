import { app, dialog, type BrowserWindow } from 'electron'
import { fetchDesktopLatest, type DesktopLatest } from './updater'
import { downloadAndInstall, restartInto } from './install'
import { recordError } from './clientLog'
import { trustedFingerprintFor } from './certificateTrust'
import { hasEmbeddedServerBinary } from './localServer'
import { compareVersions, shouldGateClient } from '../shared/version'
import { CLIENT_PROTOCOL } from '../shared/protocol'
import { resolveNativeLanguage, versionGateLabels } from './labels'

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
   * Version of the package to install. Equal to `serverVersion` for an ordinary
   * release; the PREVIOUS release's version when this release reused that
   * desktop build. The installer keys the side-by-side directory off it, and
   * installing a v0.129 app under app-0.130 makes the startup handoff delete
   * the pointer and silently revert the upgrade.
   */
  installVersion: string
  /**
   * Which side is newer. `older` means the client is behind the server (the
   * classic "upgrade the app" case); `newer` means the client is AHEAD, so
   * "download" aligns it down to the server's version. The overlay picks its
   * wording from this — "update" would be wrong for `newer`.
   */
  direction: 'older' | 'newer'
  /** Candidate URLs for the full package, best-first. */
  urls: string[]
  /** SHA256 by asset filename, from the release's SHA256SUMS (may be empty). */
  checksums: Record<string, string>
}

/**
 * Pure gate decision: given the running client version, the server's release
 * info and whether this is a packaged build, decide whether to block.
 *
 * The decision is made on the PROTOCOL number, not the version string (see
 * `shouldGateClient`): a release that did not change the shell<->server contract
 * ships the previous desktop package, so the versions differ while the client
 * remains compatible. When either side reports no protocol (a build predating
 * the field) the strict version comparison is used instead.
 *
 * Returns null when there is nothing to block on. `fetchDesktopLatest` already
 * returns null for a dev/untagged server, so `latest` here is always a real
 * release with at least one download URL.
 */
export function evaluateGate(
  clientVersion: string,
  latest: DesktopLatest,
  packaged: boolean,
  clientProtocol = 0,
): VersionGateInfo | null {
  const gate = shouldGateClient({
    clientProtocol,
    serverProtocol: latest.protocol,
    clientVersion,
    serverVersion: latest.version,
    packaged,
  })
  if (!gate) return null
  return {
    clientVersion,
    serverVersion: latest.version,
    installVersion: latest.clientVersion,
    // Equal versions can still gate on a protocol mismatch (two builds of the
    // same tag). They are treated as "older" so the prompt offers an upgrade
    // rather than describing a downgrade that is not happening.
    direction: compareVersions(clientVersion, latest.version) <= 0 ? 'older' : 'newer',
    urls: latest.urls,
    checksums: latest.checksums,
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
    // The certificate the user accepted for this server, if any. Without it a
    // self-signed server gets no upgrade offered (the request fails
    // verification) — the same outcome as before pinning existed, and the safe
    // one: this response decides which binary is downloaded and run.
    latest = await fetchDesktopLatest(url, trustedFingerprintFor(url))
  } catch (err) {
    // fetchDesktopLatest answers null for every expected failure; reaching here
    // means something unexpected, which must not vanish into a silent "no gate".
    recordError('Gate', err)
    return null
  }
  if (!latest) return null
  // The navigation may have moved on while the request was in flight.
  if (gen !== generation) return null

  return evaluateGate(app.getVersion(), latest, app.isPackaged, CLIENT_PROTOCOL)
}

/**
 * Install the server's version (the user chose "download" on the gate).
 *
 * Always the full package. On failure the gate STAYS UP with a native error
 * dialog, so the user is not dropped into an app known to be mismatched.
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

  // A fused ("all-in-one") client carries a server next to its executable, and
  // every downloadable package is the THIN one: installing it would replace the
  // client and leave the next launch without `server/`, i.e. without the server
  // it is built around. The gate can still report the mismatch; the update is a
  // new fused installer, not this path.
  if (hasEmbeddedServerBinary()) {
    const labels = versionGateLabels(resolveNativeLanguage())
    await dialog.showMessageBox(parentArg as BrowserWindow, {
      type: 'info',
      buttons: [labels.ok],
      title: labels.fusedUpdateTitle,
      message: labels.fusedUpdateMessage,
    })
    return false
  }

  try {
    await downloadAndInstall(info.urls, info.installVersion, info.checksums)
  } catch (err) {
    recordError('Install', err)
    const labels = versionGateLabels(resolveNativeLanguage())
    await dialog.showMessageBox(parentArg as BrowserWindow, {
      type: 'error',
      buttons: [labels.ok],
      title: labels.downloadFailedTitle,
      message: labels.downloadFailedMessage,
      detail: String((err as Error)?.message || err),
    })
    return false
  }

  const labels = versionGateLabels(resolveNativeLanguage())
  const { response } = await dialog.showMessageBox(parentArg as BrowserWindow, {
    type: 'info',
    buttons: [labels.restartNow, labels.later],
    defaultId: 0,
    cancelId: 1,
    title: labels.versionReadyTitle,
    message: labels.versionReadyMessage.replace('{version}', info.installVersion),
    // The pointer is already flipped, and startup reads it itself, so "later"
    // still lands on the new version on the next launch.
    detail: labels.versionReadyDetail,
  })
  // The install SUCCEEDED, so report it even when the user defers the restart:
  // the caller closes the gate, which would otherwise keep offering a download
  // of the version that is already on disk.
  if (response !== 0) return true

  try {
    await restartInto(info.installVersion)
  } catch (err) {
    await dialog.showMessageBox(parentArg as BrowserWindow, {
      type: 'error',
      buttons: [labels.ok],
      title: labels.restartFailedTitle,
      message: labels.restartFailedMessage,
      detail: String((err as Error)?.message || err),
    })
  }
  // The version is installed either way; the restart is just how it takes effect.
  return true
}

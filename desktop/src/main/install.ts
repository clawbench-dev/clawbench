import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import http from 'node:http'
import https from 'node:https'
import { Readable } from 'node:stream'
import { pipeline } from 'node:stream/promises'
import { spawn } from 'node:child_process'
import { createGunzip } from 'node:zlib'
import { app } from 'electron'
import { verifyIntegrity } from '../shared/integrity'

/**
 * Self-upgrade installer for the desktop shell.
 *
 * The previous implementation stopped after downloading and verifying the
 * tarball — it created an empty directory and called that "installed", so the
 * app could never actually upgrade itself.
 *
 * The hard constraint is that a running process cannot replace its own
 * executable, and on Windows it cannot even overwrite it. So this does NOT
 * install in place. Instead it unpacks each version into its own directory and
 * flips a POINTER file; the launcher (npm/desktop-main/bin/clawbench-desktop.js)
 * reads that pointer on startup and runs whichever version it names. Switching
 * versions is then an atomic file write plus a relaunch, and a bad build can be
 * rolled back by rewriting the pointer.
 */

/** Root of the version store: ~/.clawbench-desktop */
export function installRoot(): string {
  return path.join(os.homedir(), '.clawbench-desktop')
}

/** File holding the currently-selected version string. */
export function pointerPath(): string {
  return path.join(installRoot(), 'current')
}

/** Directory a given version unpacks into. */
export function versionDir(version: string): string {
  return path.join(installRoot(), `app-${version}`)
}

/** Read the version named by the pointer, or '' when unset/unreadable. */
export function readCurrentVersion(): string {
  try {
    return fs.readFileSync(pointerPath(), 'utf8').trim()
  } catch {
    return ''
  }
}

/**
 * Point the launcher at `version`. Written via a temp file + rename so a crash
 * mid-write cannot leave a truncated version string that the launcher would
 * then fail to resolve.
 */
export function writePointer(version: string): void {
  const target = pointerPath()
  fs.mkdirSync(path.dirname(target), { recursive: true })
  const tmp = `${target}.tmp-${process.pid}`
  fs.writeFileSync(tmp, version, 'utf8')
  fs.renameSync(tmp, target)
}

const TAR_BLOCK = 512

/**
 * Extract a gzipped tar buffer into `destDir`.
 *
 * npm tarballs wrap everything in a top-level `package/` directory, which is
 * stripped here so the result is the package root (the layout the launcher and
 * electron-builder expect).
 *
 * Only regular files and directories are handled — npm tarballs contain no
 * symlinks or device nodes, and silently following them would be a path
 * traversal risk.
 */
export async function extractTarball(tarball: Buffer, destDir: string): Promise<void> {
  const tar = await gunzip(tarball)
  let offset = 0
  // A pax extended header (type 'x') carries metadata for the entry that
  // follows it, including the real path when it exceeds the 100-byte ustar
  // field. Without this the long path would be read as its truncated form.
  let paxPath = ''

  while (offset + TAR_BLOCK <= tar.length) {
    const header = tar.subarray(offset, offset + TAR_BLOCK)
    // A zeroed block marks the end of the archive.
    if (header.every((b) => b === 0)) break

    const name = readString(header, 0, 100)
    const prefix = readString(header, 345, 155)
    const size = parseOctal(readString(header, 124, 12))
    const typeFlag = String.fromCharCode(header[156] || 0)
    offset += TAR_BLOCK

    const body = tar.subarray(offset, offset + size)
    offset += Math.ceil(size / TAR_BLOCK) * TAR_BLOCK

    if (typeFlag === 'x') {
      paxPath = readPaxPath(body)
      continue
    }
    // Global pax headers and GNU long-name entries carry nothing to extract.
    if (typeFlag === 'g' || typeFlag === 'L' || typeFlag === 'K') continue

    const fullName = paxPath || (prefix ? `${prefix}/${name}` : name)
    paxPath = ''

    // Strip the npm `package/` wrapper.
    const rel = fullName.replace(/^package\/?/, '')
    if (!rel) continue

    const target = safeJoin(destDir, rel)
    if (target === null) throw new Error(`tar entry escapes destination: ${fullName}`)

    if (typeFlag === '5' || rel.endsWith('/')) {
      fs.mkdirSync(target, { recursive: true })
      continue
    }
    // '0' and the NUL byte both mean a regular file; anything else (symlink,
    // hardlink, device) is skipped rather than materialized.
    if (typeFlag !== '0' && typeFlag !== '\0') continue

    fs.mkdirSync(path.dirname(target), { recursive: true })
    fs.writeFileSync(target, body)
  }
}

/** Read the `path=` record from a pax extended header body. */
function readPaxPath(body: Buffer): string {
  const text = body.toString('utf8')
  // Records are "<len> <key>=<value>\n", where <len> counts the whole record.
  for (const record of text.split('\n')) {
    const eq = record.indexOf('=')
    if (eq === -1) continue
    const key = record.slice(0, eq).split(' ').pop()
    if (key === 'path') return record.slice(eq + 1)
  }
  return ''
}

/**
 * Join `rel` onto `root`, returning null when the result would escape `root`.
 *
 * `rel` comes from an archive we downloaded, so it is untrusted input: a
 * crafted entry named `../../etc/cron.d/x` must not be allowed to write outside
 * the install directory.
 */
function safeJoin(root: string, rel: string): string | null {
  const target = path.resolve(root, rel)
  const rootResolved = path.resolve(root)
  if (target !== rootResolved && !target.startsWith(rootResolved + path.sep)) return null
  return target
}

function readString(buf: Buffer, start: number, len: number): string {
  const slice = buf.subarray(start, start + len)
  const end = slice.indexOf(0)
  return slice.subarray(0, end === -1 ? slice.length : end).toString('utf8')
}

/** Parse a NUL/space-terminated octal field. Returns 0 for an empty field. */
function parseOctal(s: string): number {
  const trimmed = s.replace(/\0.*$/, '').trim()
  if (!trimmed) return 0
  const n = parseInt(trimmed, 8)
  if (Number.isNaN(n)) throw new Error(`invalid tar entry size: ${JSON.stringify(s)}`)
  return n
}

async function gunzip(buf: Buffer): Promise<Buffer> {
  const chunks: Buffer[] = []
  await pipeline(Readable.from(buf), createGunzip(), async function* (source) {
    for await (const chunk of source) {
      chunks.push(chunk as Buffer)
    }
  })
  return Buffer.concat(chunks)
}

/**
 * Download, verify and unpack a desktop version, then select it.
 *
 * The unpack goes to a temp directory that is renamed into place only after it
 * completes, so a partial download or an extraction failure never leaves a
 * directory that looks installable. Returns the directory now selected.
 */
export async function downloadAndInstall(
  tarballUrl: string,
  version: string,
  integrity: string,
): Promise<string> {
  const buf = await httpGetBuffer(tarballUrl)
  if (integrity && !verifyIntegrity(buf, integrity)) {
    throw new Error('integrity verification failed')
  }

  const dest = versionDir(version)
  const staging = `${dest}.staging-${process.pid}`
  fs.rmSync(staging, { recursive: true, force: true })
  fs.mkdirSync(staging, { recursive: true })

  try {
    await extractTarball(buf, staging)
  } catch (err) {
    fs.rmSync(staging, { recursive: true, force: true })
    throw err
  }

  // Replace any prior copy of this version atomically.
  fs.rmSync(dest, { recursive: true, force: true })
  fs.renameSync(staging, dest)

  writePointer(version)
  return dest
}

/**
 * Path to the executable inside an unpacked version directory.
 *
 * Mirrors the launcher's resolution in npm/desktop-main/bin/clawbench-desktop.js:
 * electron-builder's `dir` target puts the binary at the package root on
 * Linux/Windows, and inside the .app bundle on macOS.
 */
export function executableIn(dir: string): string {
  if (process.platform === 'darwin') {
    return path.join(dir, 'ClawBench.app', 'Contents', 'MacOS', 'clawbench-desktop')
  }
  return path.join(dir, process.platform === 'win32' ? 'clawbench-desktop.exe' : 'clawbench-desktop')
}

/**
 * Start `version` as a detached process and quit this one.
 *
 * `app.relaunch()` is not usable here: it re-runs the CURRENT executable, which
 * is the version being replaced. Spawning the new binary explicitly is what
 * actually moves the user onto the upgrade — and it works whether the app was
 * started via the npm launcher or by running the unpacked binary directly.
 */
export function restartInto(version: string): void {
  const bin = executableIn(versionDir(version))
  if (!fs.existsSync(bin)) {
    throw new Error(`installed version has no executable: ${bin}`)
  }
  const child = spawn(bin, [], { detached: true, stdio: 'ignore' })
  child.unref()
  app.exit(0)
}

export function httpGetBuffer(url: string, redirectsLeft = 5): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const lib = url.startsWith('https:') ? https : http
    lib.get(url, (res) => {
      // Registries and CDNs redirect tarball URLs; without following them the
      // download would silently produce a body of zero bytes.
      const { statusCode, headers } = res
      if (statusCode && statusCode >= 300 && statusCode < 400 && headers.location) {
        if (redirectsLeft <= 0) { reject(new Error('too many redirects')); return }
        res.resume()
        resolve(httpGetBuffer(new URL(headers.location, url).toString(), redirectsLeft - 1))
        return
      }
      if (statusCode !== 200) {
        reject(new Error(`download failed: HTTP ${statusCode}`))
        return
      }
      const chunks: Buffer[] = []
      res.on('data', (c: Buffer) => chunks.push(c))
      res.on('end', () => resolve(Buffer.concat(chunks)))
    }).on('error', reject)
  })
}

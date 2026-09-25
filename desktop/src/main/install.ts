import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import http from 'node:http'
import https from 'node:https'
import { spawn } from 'node:child_process'
import { inflateRawSync, gunzipSync } from 'node:zlib'
import { app } from 'electron'
import { verifyIntegrity } from '../shared/integrity'
import { normalizeVersion } from '../shared/version'

/**
 * Self-upgrade installer for the desktop shell.
 *
 * The previous implementation stopped after downloading and verifying the
 * archive — it created an empty directory and called that "installed", so the
 * app could never actually upgrade itself.
 *
 * Upgrades are fetched as the same .zip release assets published on GitHub,
 * which is also what /api/desktop/latest points at.
 *
 * The hard constraint is that a running process cannot replace its own
 * executable, and on Windows it cannot even overwrite it. So this does NOT
 * install in place. Instead it unpacks each version into its own directory and
 * flips a POINTER file; startup reads that pointer and runs whichever version
 * it names (see handOffToPointedVersion). Switching versions is then an atomic
 * file write plus a relaunch, and a bad build can be rolled back by rewriting
 * the pointer.
 *
 * Two kinds of archive are installed through that same machinery:
 *
 *   - the FULL release zip (~150MB), which carries its own Electron runtime;
 *   - a PAYLOAD zip (~3MB) containing only `resources/`, installed over a
 *     clone of the already-running shell so the ~279MB runtime is reused
 *     (see installPayload). Linux and Windows take this path; macOS does not,
 *     because replacing `resources/app.asar` inside a signed .app breaks the
 *     code-signature seal and Apple Silicon requires a valid signature.
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

/** Zip local-file-header and central-directory signatures. */
const ZIP_LOCAL = 0x04034b50
const ZIP_CENTRAL = 0x02014b50
const ZIP_EOCD = 0x06054b50
/** End-of-central-directory is at least 22 bytes; the comment field caps at 64KiB. */
const ZIP_EOCD_MIN = 22
const ZIP_EOCD_MAX = ZIP_EOCD_MIN + 0xffff

/**
 * Extract a zip archive into `destDir`.
 *
 * Two release layouts exist, and both must land the app root in `destDir`:
 *
 *   - macOS keeps a wrapper directory (`mac/`, `mac-arm64/`), which is stripped
 *     so the result is the app root the launcher expects — the same
 *     normalization the npm tarball path did with its `package/` prefix.
 *   - Windows and Linux are archived from inside the unpacked directory, so
 *     their entries already ARE the app root and nothing is stripped.
 *
 * macOS must keep its wrapper. Without it every entry would share the top level
 * `ClawBench.app/`, and the stripping below would remove the app bundle itself
 * — after which executableIn() would find no binary. Keeping the wrapper also
 * means clients that already shipped with it (and therefore run the installer
 * that strips unconditionally) can still self-upgrade.
 *
 * The archive is read through its central directory rather than by walking
 * local headers: local headers may declare sizes in a trailing data descriptor,
 * which a naive sequential walk cannot handle.
 *
 * Unix permission bits are restored. The executable bit is load-bearing —
 * without it the Linux binary extracts as non-executable and will not start.
 */
export function extractZip(zip: Buffer, destDir: string): void {
  const entries = readCentralDirectory(zip)

  const files: ArchiveFile[] = []
  for (const entry of entries) {
    if (entry.name.endsWith('/')) continue // directories are implied by their files
    const raw = zip.subarray(entry.dataOffset, entry.dataOffset + entry.compressedSize)
    let content: Buffer
    if (entry.method === 0) {
      content = Buffer.from(raw)
    } else if (entry.method === 8) {
      content = inflateRawSync(raw)
    } else {
      throw new Error(`unsupported zip compression method ${entry.method} for ${entry.name}`)
    }
    if (content.length !== entry.uncompressedSize) {
      throw new Error(`zip entry size mismatch for ${entry.name}`)
    }
    // Zip stores the unix mode in the HIGH 16 bits of the external attributes;
    // normalize it here so both readers hand writeArchive a plain mode.
    files.push({ name: entry.name, content, mode: (entry.externalAttributes >>> 16) & 0xffff })
  }

  writeArchive(files, destDir)
}

/** One file to write, with its unix mode already normalized (0 when unknown). */
interface ArchiveFile {
  name: string
  content: Buffer
  /** Plain unix mode, e.g. 0o755, or 0 when the archive records none. */
  mode: number
}

/**
 * Write decoded archive entries under `destDir`, applying the two
 * normalizations every release layout depends on.
 *
 * Shared by the zip and tar.gz readers so the two cannot drift — in particular
 * the wrapper stripping, which is subtle enough that a divergence would install
 * a tree one directory off for only one of the archive formats.
 *
 *   - The single shared top-level directory is stripped. macOS release zips
 *     wrap in `mac/`, and npm tarballs wrap in `package/`; both must land the
 *     app root in `destDir`.
 *   - Paths are confined to `destDir`. An entry escaping it is a hard error,
 *     not a skip: a hostile archive must not be partially installed.
 */
function writeArchive(files: ArchiveFile[], destDir: string): void {
  const wrapper = commonTopLevelDir(files.map((f) => f.name))

  for (const file of files) {
    let rel = file.name
    if (wrapper) rel = rel.slice(wrapper.length)
    rel = rel.replace(/^\/+/, '')
    if (!rel) continue

    const target = safeJoin(destDir, rel)
    if (target === null) throw new Error(`archive entry escapes destination: ${file.name}`)

    fs.mkdirSync(path.dirname(target), { recursive: true })
    fs.writeFileSync(target, file.content)
    applyMode(target, file.mode)
  }
}

/** gzip magic bytes, which is how an npm tarball starts. */
const GZIP_MAGIC_0 = 0x1f
const GZIP_MAGIC_1 = 0x8b
/** "PK" — a zip's local header or end-of-central-directory signature. */
const ZIP_MAGIC_0 = 0x50
const ZIP_MAGIC_1 = 0x4b

/**
 * Whether a buffer begins like an archive we can extract.
 *
 * Used both to dispatch extraction and, more importantly, to reject a candidate
 * URL whose body is not an archive at all. A mirror can answer with an HTML
 * error or rate-limit page under HTTP 200; accepting that body would waste the
 * candidate and, because the payload path falls back to the ~150MB full
 * download on any failure, turn a retryable miss into an expensive one.
 */
export function looksLikeArchive(buf: Buffer): boolean {
  if (buf.length < 4) return false
  if (buf[0] === GZIP_MAGIC_0 && buf[1] === GZIP_MAGIC_1) return true
  return buf[0] === ZIP_MAGIC_0 && buf[1] === ZIP_MAGIC_1
}

/**
 * Extract a release archive, dispatching on its magic bytes.
 *
 * Two formats reach this point: the GitHub release zips, and the npm tarballs
 * served by a registry mirror. The client does not know (and should not need to
 * know) which candidate URL produced the body it holds, so the format is
 * detected from the bytes rather than from the URL's extension.
 */
export function extractArchive(buf: Buffer, destDir: string): void {
  if (buf.length >= 2 && buf[0] === GZIP_MAGIC_0 && buf[1] === GZIP_MAGIC_1) {
    extractTarGz(buf, destDir)
    return
  }
  if (buf.length >= 2 && buf[0] === ZIP_MAGIC_0 && buf[1] === ZIP_MAGIC_1) {
    extractZip(buf, destDir)
    return
  }
  throw new Error('unrecognized archive format (expected zip or tar.gz)')
}

/** tar header size, and the block size all tar data is padded to. */
const TAR_BLOCK = 512

/**
 * Extract a gzipped tar archive into `destDir`.
 *
 * Written by hand rather than pulled from the `tar` package: the only archives
 * this ever sees are npm tarballs we publish ourselves, and `tar` is currently
 * only a transitive devDependency of electron-builder, so it would not ship in
 * app.asar without being promoted to a real dependency.
 *
 * Scope is deliberately narrow — enough for `npm pack` output, no more:
 *
 *   - ustar `name` + `prefix` concatenation (npm splits long paths this way;
 *     verified on a real payload: 121 entries, all regular files, no PAX);
 *   - PAX `x` headers, since npm falls back to them for a path that cannot be
 *     split (a 200+ char segment), and GNU `L`/`K` long names;
 *   - regular files and directories; anything else (symlink, device) throws,
 *     because silently skipping it would install a quietly incomplete tree.
 */
export function extractTarGz(gz: Buffer, destDir: string): void {
  const tar = gunzipSync(gz)
  const files: ArchiveFile[] = []

  let offset = 0
  // A PAX/GNU override applies to the entry that follows it.
  let pendingName: string | null = null
  let pendingMode: number | null = null

  while (offset + TAR_BLOCK <= tar.length) {
    const header = tar.subarray(offset, offset + TAR_BLOCK)
    // Two consecutive zero blocks end the archive; a single one ends it too in
    // practice, and the loop stops when the name field is empty.
    if (isZeroBlock(header)) break

    const nameField = readCString(header, 0, 100)
    const modeField = readOctal(header, 100, 8)
    const size = readOctal(header, 124, 12)
    const typeflag = header[156] === 0 ? '0' : String.fromCharCode(header[156])
    const prefix = readCString(header, 345, 155)

    if (size === null) throw new Error(`tar: malformed size field for ${nameField || '(unnamed)'}`)

    const dataStart = offset + TAR_BLOCK
    const dataEnd = dataStart + size
    // Bounds are checked BEFORE the type is acted on, so a truncated archive
    // cannot slip through as a partial install: cutting resources/ but keeping
    // payload.json would otherwise satisfy the manifest check downstream.
    if (dataEnd > tar.length) {
      throw new Error(`tar: truncated archive (entry ${nameField || '(unnamed)'} runs past the end)`)
    }
    const nextOffset = dataStart + Math.ceil(size / TAR_BLOCK) * TAR_BLOCK

    // The full path is prefix + '/' + name; the prefix field is NUL-padded, so
    // it must be stripped of NULs rather than space-trimmed.
    const fullName = pendingName ?? (prefix ? `${prefix}/${nameField}` : nameField)
    const mode = pendingMode ?? modeField ?? 0

    switch (typeflag) {
      case '0': // regular file (historical implementations also write NUL)
        if (!fullName) throw new Error('tar: file entry with an empty name')
        files.push({ name: fullName, content: Buffer.from(tar.subarray(dataStart, dataEnd)), mode })
        pendingName = null
        pendingMode = null
        break

      case '5': // directory — implied by its files, nothing to do
      case 'g': // global PAX header — no meaning for us
        pendingName = null
        pendingMode = null
        break

      case 'x': // PAX extended header for the NEXT entry
      case 'L': // GNU long name for the next entry
      case 'K': // GNU long link name for the next entry
        {
          const body = tar.subarray(dataStart, dataEnd).toString('utf8')
          if (typeflag === 'L') {
            pendingName = body.replace(/\0+$/, '')
          } else if (typeflag === 'x') {
            const pax = parsePaxRecords(body)
            if (pax.path) pendingName = pax.path
            if (pax.mode !== undefined) pendingMode = pax.mode
          }
          // 'K' only names a link target, which we do not support.
        }
        break

      default:
        throw new Error(`tar: unsupported entry type '${typeflag}' for ${fullName || '(unnamed)'}`)
    }

    offset = nextOffset
  }

  writeArchive(files, destDir)
}

/** True when a 512-byte block is entirely NUL — the archive's end marker. */
function isZeroBlock(block: Buffer): boolean {
  for (const b of block) if (b !== 0) return false
  return true
}

/** Read a NUL-terminated string field from a tar header. */
function readCString(buf: Buffer, start: number, len: number): string {
  const slice = buf.subarray(start, start + len)
  const end = slice.indexOf(0)
  return slice.subarray(0, end === -1 ? slice.length : end).toString('utf8')
}

/**
 * Parse a tar numeric field (octal), tolerating space/NUL padding, or null when
 * the field is not a usable number.
 */
function readOctal(buf: Buffer, start: number, len: number): number | null {
  const raw = readCString(buf, start, len).trim()
  if (raw === '') return 0
  if (!/^[0-7]+$/.test(raw)) return null
  return parseInt(raw, 8)
}

/**
 * Parse PAX extended-header records.
 *
 * The format is `LEN key=value\n` where LEN counts the whole record including
 * its own digits — so records must be walked by length, not split on '='.
 */
function parsePaxRecords(body: string): { path?: string; mode?: number } {
  const out: { path?: string; mode?: number } = {}
  let i = 0
  while (i < body.length) {
    const space = body.indexOf(' ', i)
    if (space === -1) break
    const len = parseInt(body.slice(i, space), 10)
    if (!Number.isFinite(len) || len <= 0 || i + len > body.length) break

    const record = body.slice(space + 1, i + len).replace(/\n$/, '')
    const eq = record.indexOf('=')
    if (eq > 0) {
      const key = record.slice(0, eq)
      const value = record.slice(eq + 1)
      if (key === 'path') out.path = value
      else if (key === 'mode') {
        const m = parseInt(value, 8)
        if (Number.isFinite(m)) out.mode = m
      }
    }
    i += len
  }
  return out
}

interface ZipEntry {
  name: string
  method: number
  compressedSize: number
  uncompressedSize: number
  /** Byte offset of the entry's payload in the archive. */
  dataOffset: number
  /** High 16 bits of the central-directory external attributes (unix mode). */
  externalAttributes: number
}

/** Parse the central directory into entry descriptors. */
function readCentralDirectory(zip: Buffer): ZipEntry[] {
  const eocd = findEOCD(zip)
  if (eocd < 0) throw new Error('not a zip archive: no end-of-central-directory record')

  const count = zip.readUInt16LE(eocd + 10)
  let offset = zip.readUInt32LE(eocd + 16)
  const entries: ZipEntry[] = []

  for (let i = 0; i < count; i++) {
    if (offset + 46 > zip.length || zip.readUInt32LE(offset) !== ZIP_CENTRAL) {
      throw new Error('corrupt zip: bad central directory header')
    }
    const method = zip.readUInt16LE(offset + 10)
    const compressedSize = zip.readUInt32LE(offset + 20)
    const uncompressedSize = zip.readUInt32LE(offset + 24)
    const nameLen = zip.readUInt16LE(offset + 28)
    const extraLen = zip.readUInt16LE(offset + 30)
    const commentLen = zip.readUInt16LE(offset + 32)
    const externalAttributes = zip.readUInt32LE(offset + 38)
    const localOffset = zip.readUInt32LE(offset + 42)
    const name = zip.subarray(offset + 46, offset + 46 + nameLen).toString('utf8')

    // The local header repeats the name/extra lengths; its payload starts after
    // them, and those lengths can differ from the central directory's.
    if (localOffset + 30 > zip.length || zip.readUInt32LE(localOffset) !== ZIP_LOCAL) {
      throw new Error(`corrupt zip: bad local header for ${name}`)
    }
    const localNameLen = zip.readUInt16LE(localOffset + 26)
    const localExtraLen = zip.readUInt16LE(localOffset + 28)
    const dataOffset = localOffset + 30 + localNameLen + localExtraLen

    entries.push({ name, method, compressedSize, uncompressedSize, dataOffset, externalAttributes })
    offset += 46 + nameLen + extraLen + commentLen
  }
  return entries
}

/** Locate the end-of-central-directory record, scanning back over any comment. */
function findEOCD(zip: Buffer): number {
  const from = Math.max(0, zip.length - ZIP_EOCD_MAX)
  for (let i = zip.length - ZIP_EOCD_MIN; i >= from; i--) {
    if (zip.readUInt32LE(i) === ZIP_EOCD) return i
  }
  return -1
}

/**
 * Return the single top-level directory shared by every entry, or '' when the
 * entries do not share one (then paths are used as-is).
 *
 * Requiring ALL entries to share it matters: a stray top-level file must not be
 * silently dropped by stripping a prefix that only some entries have.
 */
function commonTopLevelDir(names: string[]): string {
  let candidate = ''
  for (const name of names) {
    const slash = name.indexOf('/')
    if (slash <= 0) return ''
    const top = name.slice(0, slash + 1)
    if (candidate === '') candidate = top
    else if (candidate !== top) return ''
  }
  return candidate
}

/**
 * Apply unix permission bits to an extracted file.
 *
 * `mode` is the plain unix mode (e.g. 0o755) — each reader normalizes its own
 * archive's encoding into that before calling here. Best-effort: on Windows
 * chmod cannot set the executable bit and is skipped.
 */
function applyMode(target: string, mode: number): void {
  if (mode === 0 || process.platform === 'win32') return
  try {
    fs.chmodSync(target, mode & 0o777)
  } catch {
    // A filesystem that cannot represent the mode (e.g. FAT) is not fatal.
  }
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

/**
 * Download, verify and unpack a desktop version, then select it.
 *
 * The unpack goes to a temp directory that is renamed into place only after it
 * completes, so a partial download or an extraction failure never leaves a
 * directory that looks installable. Returns the directory now selected.
 *
 * `urls` is an ordered list of candidates for the same archive (mirror first
 * for China, direct github.com last). Each is tried in turn so one dead mirror
 * degrades the upgrade rather than failing it.
 */
export async function downloadAndInstall(
  urls: string | string[],
  version: string,
  integrity: string,
): Promise<string> {
  const buf = await downloadFirstAvailable(Array.isArray(urls) ? urls : [urls])
  if (integrity && !verifyIntegrity(buf, integrity)) {
    throw new Error('integrity verification failed')
  }

  const dest = versionDir(version)
  const staging = `${dest}.staging-${process.pid}`
  fs.rmSync(staging, { recursive: true, force: true })
  fs.mkdirSync(staging, { recursive: true })

  try {
    extractArchive(buf, staging)
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
 * Try each candidate URL in order, returning the first body that downloads AND
 * looks like an archive.
 *
 * The archive check matters because a candidate that is not a real archive must
 * not end the walk. A mirror answering a miss or a rate limit can return an
 * HTML page under HTTP 200; accepting it would fail extraction a moment later,
 * and for a payload that failure means falling all the way back to the ~150MB
 * full download instead of simply trying the next candidate. Rejecting the body
 * here keeps the fallback proportional to the problem.
 */
export async function downloadFirstAvailable(urls: string[]): Promise<Buffer> {
  const errs: string[] = []
  for (const url of urls) {
    let buf: Buffer
    try {
      buf = await httpGetBuffer(url)
    } catch (err) {
      errs.push(`${url}: ${(err as Error)?.message || err}`)
      continue
    }
    if (!looksLikeArchive(buf)) {
      errs.push(`${url}: response is not a zip or tar.gz archive`)
      continue
    }
    return buf
  }
  throw new Error(`all download sources failed:\n${errs.join('\n')}`)
}

/** Name of the manifest a payload archive carries at its root. */
export const PAYLOAD_MANIFEST = 'payload.json'

/**
 * Sidecar at an install's app root recording the shell's identity.
 *
 * Written by CI into the unpacked build (`desktop/scripts/shell-fingerprint.mjs`)
 * and shipped inside the full package, so every full install carries it. The
 * payload archive records the same value in `payload.json`, and an install
 * compares the two.
 *
 * It deliberately sits OUTSIDE `resources/`: that keeps it out of the payload
 * (so it cannot be forged by one) and means cloneTree copies it from the shell
 * during a payload install, propagating the identity forward automatically.
 */
export const SHELL_FINGERPRINT_FILE = 'shell-fingerprint.txt'

/** The directory the running app's own files live in. */
export function appRoot(): string {
  return path.dirname(process.resourcesPath)
}

/**
 * Read the shell fingerprint an install recorded, or '' when absent.
 *
 * '' is expected for installs created before this file existed; callers must
 * treat that as "cannot verify" and refuse the payload, not as a match.
 */
export function readShellFingerprint(dir: string): string {
  try {
    return fs.readFileSync(path.join(dir, SHELL_FINGERPRINT_FILE), 'utf8').trim()
  } catch {
    return ''
  }
}

/**
 * Raised when a payload cannot be used against the installed shell — a
 * different Electron ABI, or a payload built for a different app. Callers
 * treat this as "fall back to the full download", not as a hard failure.
 */
export class PayloadIncompatibleError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'PayloadIncompatibleError'
  }
}

/**
 * Major component of an Electron version, or '' when it cannot be parsed.
 *
 * Native modules (`sshcrypto.node`, `cpufeatures.node`) are compiled against
 * Electron's ABI, which only changes on a MAJOR bump. Gating on the major
 * component is therefore what actually decides whether reusing the shell is
 * safe; comparing the full string would reject payloads unnecessarily on
 * patch releases that carry no ABI change.
 */
export function electronMajor(version: string): string {
  const m = /^(\d+)/.exec(version.trim())
  return m ? m[1] : ''
}

/**
 * Copy `src` into `dst`, skipping every path that already exists under `dst`.
 *
 * The skip rule is the whole trick, and it is what makes payload installs safe:
 * the caller extracts the payload FIRST, so every path the payload provides is
 * already present and is left untouched. What remains — the Electron runtime —
 * is hardlinked from the installed shell, which costs no extra disk and no
 * copy time on the same volume.
 *
 * Skipping on existence rather than on a precomputed path set is deliberate:
 * it needs no separator normalization (zip entries use `/`, `path.relative`
 * yields `\` on Windows) and cannot drift from what the archive actually
 * contained. It also makes the dangerous ordering impossible — cloning first
 * and extracting second would write through a hardlink into the OLD version's
 * files, mutating the install the user is currently running.
 */
export function cloneTree(src: string, dst: string): void {
  for (const entry of fs.readdirSync(src, { withFileTypes: true })) {
    const from = path.join(src, entry.name)
    const to = path.join(dst, entry.name)

    if (fs.existsSync(to)) continue // already provided by the payload

    if (entry.isDirectory()) {
      fs.mkdirSync(to, { recursive: true })
      cloneTree(from, to)
    } else if (entry.isFile()) {
      try {
        fs.linkSync(from, to)
      } catch {
        // EXDEV (different volume), EPERM (filesystem without hardlinks),
        // EMLINK (link limit). A real copy always works, so degrade rather
        // than fail the upgrade. copyFileSync preserves the mode, which the
        // Linux binary's executable bit depends on.
        fs.copyFileSync(from, to)
      }
    } else {
      // A symlink or device node here would mean the release layout changed.
      // Skipping it silently would ship an install that is quietly missing
      // files, so refuse instead.
      throw new Error(`unsupported entry in shell: ${from}`)
    }
  }
}

/**
 * Install a payload archive over a clone of `shellRoot`, then select it.
 *
 * `shellRoot` is the app root of the version currently running (see appRoot).
 * Its Electron runtime is reused; only `resources/` comes from the archive.
 *
 * The archive is staged exactly like the full install: unpack to a temp
 * directory, verify it, clone the shell around it, then `rm -rf` + rename into
 * place. `rm -rf` before the rename is safe even when `dest` is a previous
 * payload install, because the rename is what makes the switch atomic — a
 * crash at any earlier point leaves only a staging directory, never a
 * half-populated version the pointer could name.
 *
 * Returns the directory now selected.
 */
export async function installPayload(
  urls: string | string[],
  version: string,
  shellRoot: string,
  electron: string = process.versions.electron ?? '',
): Promise<string> {
  const dest = versionDir(version)
  // Refuse to install over the version we are running from: the `rm -rf` below
  // would delete the live binary out from under this process.
  if (path.resolve(dest) === path.resolve(shellRoot)) {
    throw new PayloadIncompatibleError(`payload target is the running install: ${dest}`)
  }

  const buf = await downloadFirstAvailable(Array.isArray(urls) ? urls : [urls])

  const staging = `${dest}.staging-${process.pid}`
  fs.rmSync(staging, { recursive: true, force: true })
  fs.mkdirSync(staging, { recursive: true })

  try {
    extractArchive(buf, staging)

    const manifestPath = path.join(staging, PAYLOAD_MANIFEST)
    if (!fs.existsSync(manifestPath)) {
      throw new Error(`payload archive has no ${PAYLOAD_MANIFEST}`)
    }
    let manifest: { electron?: string; shell?: string }
    try {
      manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
    } catch {
      throw new Error(`payload ${PAYLOAD_MANIFEST} is not valid JSON`)
    }

    const built = electronMajor(String(manifest.electron ?? ''))
    const running = electronMajor(electron)
    if (!built || !running || built !== running) {
      throw new PayloadIncompatibleError(
        `payload targets Electron ${manifest.electron || '?'}, running ${electron || '?'}`,
      )
    }

    // The ABI gate above only covers Electron. The shell's IDENTITY — appId,
    // productName, executableName, the app icon — is baked into the executable
    // and packaging, so a payload can never update it. Reusing a shell built
    // with a different identity would silently leave those stale (an unchanged
    // icon is the visible symptom), with nothing at runtime able to detect it.
    //
    // Both sides must be present and equal. An absent side means "cannot
    // verify" — an install predating the sidecar, or a payload from an older
    // CI — and is refused rather than assumed compatible.
    const wanted = String(manifest.shell ?? '')
    const installed = readShellFingerprint(shellRoot)
    if (!wanted || !installed) {
      throw new PayloadIncompatibleError(
        `shell fingerprint unavailable (payload ${wanted || 'none'}, installed ${installed || 'none'})`,
      )
    }
    if (wanted !== installed) {
      throw new PayloadIncompatibleError(
        `payload targets shell ${wanted}, installed ${installed}`,
      )
    }

    // The manifest is metadata, not part of the app; leaving it behind would
    // put a stray file in the install root. The shell fingerprint sidecar, by
    // contrast, is NOT removed: it is part of the shell and cloneTree brings it
    // forward from shellRoot, so this install keeps advertising the same
    // identity and the next payload install can verify against it.
    fs.rmSync(manifestPath, { force: true })

    // An npm tarball carries a root package.json that a release zip does not,
    // and the shell root has none (electron-builder keeps it inside app.asar).
    // Removing it keeps the installed tree identical whichever candidate URL
    // won, so a mirror-served upgrade and a github-served one cannot diverge —
    // and cloneTree would otherwise treat it as a payload-provided file.
    const npmWrapperPkg = path.join(staging, 'package.json')
    if (fs.existsSync(npmWrapperPkg)) fs.rmSync(npmWrapperPkg, { force: true })

    cloneTree(shellRoot, staging)
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
 * electron-builder's `dir` target puts the binary at the package root on
 * Linux/Windows, and inside the .app bundle on macOS. Both the install path and
 * the startup handoff resolve it through here so they cannot disagree.
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

/**
 * Decide which installed version should be running, given the pointer and the
 * currently-running version.
 *
 * Returns the version to switch to, or '' when the current process should just
 * keep going. Pure (no filesystem or Electron access) so the decision table is
 * unit-testable; the caller performs the side effect.
 *
 * The pointer is written by `downloadAndInstall` and used to be read only by an
 * npm launcher that has since been removed along with desktop npm publishing.
 * Users now run the unpacked binary directly, so the app resolves the pointer
 * itself; without that an upgrade would be written but never applied, and the
 * next cold start would silently revert.
 */
export function selectStartupVersion(
  pointed: string,
  running: string,
): string {
  if (!pointed) return ''
  // Already the pointed-at version: nothing to do. This is the normal case
  // after a restart, and it is also what stops the handoff from looping.
  //
  // Compared NORMALIZED, because the two sides spell the same version
  // differently: the pointer holds the server's `v0.99.1` (git describe) while
  // `app.getVersion()` reports `0.99.1` (CI strips the prefix from
  // desktop/package.json). A raw comparison reported "different", which sent
  // this into handOffToPointedVersion's self-reference guard — and that guard
  // DELETES the pointer, so the upgrade silently reverted on the second cold
  // start. The returned value stays the RAW `pointed`, because versionDir()
  // must resolve the directory the pointer actually names.
  if (normalizeVersion(pointed) === normalizeVersion(running)) return ''
  return pointed
}

/**
 * If the pointer names a different, actually-installed version, start that one
 * and quit this process.
 *
 * Called once at startup. Guards, in order:
 *   - no pointer / same version          → nothing to do
 *   - the pointed version's directory or executable is missing (a deleted or
 *     half-written upgrade) → clear the pointer and continue with THIS version,
 *     so a broken upgrade can never leave the user unable to start the app
 *   - the pointed binary is this very file (e.g. someone unpacked a release on
 *     top of the version store) → continue rather than re-exec forever
 *
 * Returns true when a handoff was started (the caller should not continue
 * initialising; this process is about to exit).
 */
export function handOffToPointedVersion(runningVersion: string): boolean {
  const pointed = readCurrentVersion()
  if (selectStartupVersion(pointed, runningVersion) === '') return false

  const bin = executableIn(versionDir(pointed))
  let sameFile = false
  try {
    sameFile = fs.realpathSync(bin) === fs.realpathSync(process.execPath)
  } catch {
    sameFile = false
  }
  if (!fs.existsSync(bin) || sameFile) {
    // Broken or self-referential pointer: drop it and run what we have.
    try { fs.rmSync(pointerPath(), { force: true }) } catch { /* best effort */ }
    return false
  }

  const child = spawn(bin, [], { detached: true, stdio: 'ignore' })
  child.unref()
  return true
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

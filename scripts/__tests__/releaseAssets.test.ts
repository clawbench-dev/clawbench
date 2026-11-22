import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Regression guard for release asset filenames.
 *
 * Every asset published to a GitHub Release carries the release tag in its
 * filename (`clawbench-linux-amd64-v0.99.1.zip`), so a file in a Downloads
 * folder is self-identifying. That property is easy to lose one job at a time:
 * a new build job copied from an old one, or a hand-edited `zip` line, silently
 * ships an unversioned asset while the release still looks complete.
 *
 * The failure mode is worse than a cosmetic inconsistency. The tag is
 * interpolated independently in the packaging step and in the upload's
 * `files:` pattern; when they disagree, the pattern matches no file and
 * softprops/action-gh-release uploads *nothing* — the job stays green and the
 * release ships without that platform. So the guard checks both sides, and that
 * they use the same variable.
 *
 * Scanned as text rather than parsed as YAML: `js-yaml` is only a transitive
 * dependency here, and the existing versionCode guard reads these same workflow
 * files the same way. The checks are structural (which step owns which line)
 * rather than regex-over-the-whole-file, so a comment mentioning an asset name
 * cannot satisfy them.
 */

/** Locate a repo file, tolerating either cwd the suite may run from. */
function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const RELEASE_YML = repoPath('.github/workflows/release.yml')

function releaseYml(): string {
  return readFileSync(RELEASE_YML, 'utf8')
}

/** Whether a repo-relative path exists, tolerating either cwd. */
function repoFileExists(rel: string): boolean {
  return [process.cwd(), resolve(process.cwd(), '..')].some((base) => existsSync(resolve(base, rel)))
}

/**
 * Split a workflow into job bodies, keyed by job name.
 *
 * Job names sit at two-space indentation directly under `jobs:`; everything
 * more indented belongs to that job. Scanned as text rather than parsed as
 * YAML, for the same reason as the rest of this file (js-yaml is only a
 * transitive dependency).
 */
function jobs(src: string): Record<string, string> {
  const out: Record<string, string> = {}
  const lines = src.split('\n')
  let name = ''
  let body: string[] = []
  for (const line of lines) {
    const m = line.match(/^ {2}([a-z0-9-]+):\s*$/)
    if (m) {
      if (name) out[name] = body.join('\n')
      name = m[1]
      body = []
    } else if (name) {
      body.push(line)
    }
  }
  if (name) out[name] = body.join('\n')
  return out
}

/**
 * The `files:` values of every softprops/action-gh-release step.
 *
 * Handles both the inline form (`files: path/x.zip`) and the block form
 * (`files: |` followed by indented entries). Scanning stops at the next step so
 * a `files:` belonging to an unrelated action is never attributed here.
 */
function releaseUploadTargets(src: string): string[] {
  const lines = src.split('\n')
  const out: string[] = []

  for (let i = 0; i < lines.length; i++) {
    if (!lines[i].includes('softprops/action-gh-release')) continue

    for (let j = i + 1; j < lines.length; j++) {
      // The next step begins — this release step had no `files:` at all.
      if (/^\s*-\s/.test(lines[j])) break

      const m = lines[j].match(/^(\s*)files:\s*(.*)$/)
      if (!m) continue

      const indent = m[1].length
      const inline = m[2].trim()
      if (inline && inline !== '|' && inline !== '>') {
        out.push(inline)
      } else {
        for (let k = j + 1; k < lines.length; k++) {
          const l = lines[k]
          if (l.trim() === '') continue
          const lineIndent = l.length - l.trimStart().length
          if (lineIndent <= indent) break
          out.push(l.trim())
        }
      }
      break
    }
  }
  return out
}

/**
 * Filenames written by the packaging steps: `zip -r "<name>" ...` on the POSIX
 * jobs and `Compress-Archive ... -DestinationPath "<name>"` on the Windows ones.
 */
function packagingTargets(src: string): string[] {
  const out: string[] = []
  for (const line of src.split('\n')) {
    const zip = line.match(/zip\s+-r\s+"([^"]+)"/)
    if (zip) out.push(zip[1])
    const compress = line.match(/-DestinationPath\s+"([^"]+)"/)
    if (compress) out.push(compress[1])
  }
  return out
}

describe('release.yml — every published asset carries the version', () => {
  it('defines the tag once at workflow level', () => {
    // A single source for the tag is what keeps the packaging step and the
    // upload pattern in agreement. Per-step copies would drift silently.
    expect(releaseYml()).toMatch(/^env:\n\s+RELEASE_TAG:\s*\$\{\{\s*github\.ref_name\s*\}\}/m)
  })

  it('uploads at least one asset per release step', () => {
    // A step whose `files:` pattern matches nothing uploads nothing and stays
    // green, so an empty extraction means the parser (or the workflow) lost a
    // target — fail loudly rather than pass vacuously. The floor is the current
    // count (11 release steps, one of which uploads two macOS archives = 12
    // assets); it only needs to rise, never to match exactly, so adding a
    // platform does not require editing this number.
    const targets = releaseUploadTargets(releaseYml())
    expect(targets.length).toBeGreaterThanOrEqual(12)
  })

  it('names every uploaded asset with the tag', () => {
    for (const target of releaseUploadTargets(releaseYml())) {
      expect(target, `asset uploaded without a version: ${target}`).toContain('RELEASE_TAG')
    }
  })

  it('names every packaged archive with the tag', () => {
    // The other half of the contract: the file the packaging step writes must
    // itself be versioned, not just the path the upload looks for. The floor is
    // the current count (11 `zip -r` / `Compress-Archive` invocations); it only
    // needs to rise. It dropped by three along with the payload zips.
    const targets = packagingTargets(releaseYml())
    expect(targets.length).toBeGreaterThanOrEqual(11)
    for (const target of targets) {
      expect(target, `archive written without a version: ${target}`).toContain('RELEASE_TAG')
    }
  })

  it('uses the same tag variable on both sides of each upload', () => {
    // Guards the silent-empty-upload case directly: the packaging step's
    // `${RELEASE_TAG}` and the upload's `${{ env.RELEASE_TAG }}` must resolve to
    // the same value. Both forms must mention RELEASE_TAG, which the two checks
    // above establish; this one pins the shell/expression spellings so a typo
    // like `${RELEASE_VERSION}` cannot pass by mentioning "RELEASE" alone.
    const src = releaseYml()
    const shell = packagingTargets(src)
    const uploads = releaseUploadTargets(src)
    expect(shell.every((t) => /\$\{RELEASE_TAG\}|\$env:RELEASE_TAG/.test(t))).toBe(true)
    expect(uploads.every((t) => /\$\{\{\s*env\.RELEASE_TAG\s*\}\}/.test(t))).toBe(true)
  })

  it('keeps the Android APK embed path unversioned', () => {
    // The one deliberate exception: the APK's build output filename is fixed by
    // Gradle and by the go:embed path (assets/clawbench-android.apk), so the
    // versioned asset is a *copy* staged for the release. Renaming the build
    // output instead would break /api/apk for every user.
    const src = releaseYml()
    expect(src).toContain('assets/clawbench-android.apk')
    // Both halves of the staging: the copy that creates the versioned file and
    // the upload that publishes it. Dropping the copy alone would leave the
    // upload pattern matching nothing — a green job and no APK on the release.
    expect(src).toMatch(/cp android\/app\/build\/outputs\/apk\/release\/clawbench-android\.apk \\\n\s+"android\/app\/build\/outputs\/apk\/release\/clawbench-android-\$\{RELEASE_TAG\}\.apk"/)
    expect(src).toMatch(/clawbench-android-\$\{\{\s*env\.RELEASE_TAG\s*\}\}\.apk/)
  })
})


describe('desktop asset names match the Go download URLs', () => {
  it('builds the Go asset name from the tag too', () => {
    // The server hands the desktop client these URLs, so the two sides must
    // agree. A Go constant without the tag would 404 for every user even though
    // the URL is well-formed — and the Go test that pins the names would need
    // updating in lockstep, which is the point of asserting it here as well.
    const go = readFileSync(repoPath('internal/service/desktop_upgrade.go'), 'utf8')
    expect(go).toMatch(/base\s*\+\s*"-"\s*\+\s*tag\s*\+\s*"\.zip"/)
  })

  it('has no unversioned desktop asset name left in the Go source', () => {
    // The pre-fix shape, asserted gone so it cannot be reintroduced as a
    // literal map value.
    const go = readFileSync(repoPath('internal/service/desktop_upgrade.go'), 'utf8')
    expect(go).not.toMatch(/clawbench-desktop-[a-z0-9-]+\.zip"/)
  })
})

describe('all-in-one (integrated) package name matches the Go download URLs', () => {
  it('release.yml assembles every basename the server advertises', () => {
    // An integrated install upgrades by downloading this exact asset; a drift
    // here makes every in-app upgrade 404 while the URL still looks well-formed.
    // Every desktop platform publishes one, so the server must advertise
    // exactly the set release.yml builds — neither a missing entry (an install
    // that can never upgrade) nor an extra one (a dead URL).
    const src = readFileSync(repoPath('.github/workflows/release.yml'), 'utf8')

    const go = readFileSync(repoPath('internal/service/desktop_upgrade.go'), 'utf8')
    const block =
      /desktopIntegratedAssetBase\s*=\s*map\[string\]string\{([\s\S]*?)\n\}/.exec(go)?.[1] ?? ''
    expect(block).not.toBe('')

    const platforms = [
      'platformWindowsAMD64',
      'platformLinuxAMD64',
      'platformLinuxARM64',
      'platformDarwinAMD64',
      'platformDarwinARM64',
    ]
    const bases = [
      'clawbench-desktop-windows-x64-local',
      'clawbench-desktop-linux-x64-local',
      'clawbench-desktop-linux-arm64-local',
      'clawbench-desktop-darwin-x64-local',
      'clawbench-desktop-darwin-arm64-local',
    ]
    platforms.forEach((platform, i) => {
      expect(block, `${platform} must be advertised`).toContain(platform)
      // The Go map and the workflow must name the same file.
      expect(block).toContain(bases[i])
      expect(src, `${bases[i]} must be assembled by release.yml`).toContain(bases[i])
    })
  })
})

/**
 * Desktop payloads are gone. The shell always downloads the full package, so
 * there is no payload to stage, publish to npm, or advertise from the server.
 * These guards are deliberately inverse: any of these names reappearing means
 * the old path is creeping back.
 */
describe('desktop payloads are removed', () => {
  it('has no payload assets or packaging commands in the workflow', () => {
    expect(releaseYml()).not.toContain('-payload')
    for (const target of releaseUploadTargets(releaseYml())) {
      expect(target).not.toContain('-payload')
    }
    for (const target of packagingTargets(releaseYml())) {
      expect(target).not.toContain('-payload')
    }
  })

  it('has no payload packages or scripts left in the tree', () => {
    expect(repoFileExists('npm/desktop-payloads')).toBe(false)
    expect(repoFileExists('desktop/scripts/stage-payload.mjs')).toBe(false)
    expect(repoFileExists('desktop/scripts/shell-fingerprint.mjs')).toBe(false)
  })
})

describe('desktop SHA256SUMS', () => {
  it('collects every desktop zip as an artifact for the checksum job', () => {
    // The checksum job can only hash what the build jobs upload. Each desktop
    // job uploads a `desktop-*-zip` artifact; a missing one silently drops that
    // platform from the manifest.
    const src = releaseYml()
    for (const name of [
      'desktop-linux-zip',
      'desktop-linux-arm64-zip',
      'desktop-windows-zip',
      'desktop-macos-zip',
    ]) {
      expect(src, `missing artifact ${name}`).toContain(name)
    }
  })

  it('hashes the zips and attaches the manifest to the release', () => {
    const job = jobs(releaseYml())['desktop-checksums']
    expect(job, 'missing desktop-checksums job').toBeTruthy()
    expect(job).toContain('sha256sum *.zip')
    expect(job).toContain('gh release upload')
    expect(job).toContain('SHA256SUMS')
    // It must wait for every desktop job, or it hashes a partial set.
    for (const dep of [
      'build-desktop-linux',
      'build-desktop-linux-arm64',
      'build-desktop-windows',
      'build-desktop-macos',
    ]) {
      expect(job, `desktop-checksums must need ${dep}`).toContain(dep)
    }
  })

  it('the server fetches the manifest directly from github.com, not a proxy', () => {
    const go = readFileSync(repoPath('internal/service/desktop_checksums.go'), 'utf8')
    expect(go).toContain('https://github.com/')
    expect(go).toContain('SHA256SUMS')
    expect(go).not.toContain('gh-proxy')
  })
})

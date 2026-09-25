import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve, join } from 'node:path'

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

/** Desktop jobs that publish a payload archive. */
const PAYLOAD_JOBS = ['build-desktop-linux', 'build-desktop-linux-arm64', 'build-desktop-windows']

/** Payload asset basenames the Go server maps to a platform. */
function payloadBasesInGo(): string[] {
  const go = readFileSync(repoPath('internal/service/desktop_upgrade.go'), 'utf8')
  const block = /desktopPayloadAssetBase\s*=\s*map\[string\]string\{([\s\S]*?)\n\}/.exec(go)?.[1] ?? ''
  return [...block.matchAll(/"([^"]*payload)"/g)].map((m) => m[1]).sort()
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
    // count (11 release steps, one of which uploads two macOS archives); it only
    // needs to rise, never to match exactly, so adding a platform does not
    // require editing this number.
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
    // the current count (10 `zip`/`Compress-Archive` invocations plus the APK
    // copy); it only needs to rise.
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

/**
 * Payload archives (the incremental-upgrade path) are the same app without the
 * Electron runtime. They are published by the desktop jobs and advertised by
 * the Go server, so the basenames have to agree across the language boundary.
 *
 * The failure mode is nastier than for the full package: the client treats any
 * payload problem as "fall back to the full download", so a name mismatch does
 * not 404 loudly — it silently makes every upgrade re-download ~150MB again,
 * which is exactly the cost this feature exists to remove. Hence a direct
 * comparison of the two sides rather than a tag-presence check.
 */
describe('payload asset names agree between release.yml and the Go server', () => {
  /** Basenames the workflow zips, e.g. "clawbench-desktop-linux-x64-payload". */
  function payloadBasesInWorkflow(): string[] {
    return packagingTargets(releaseYml())
      .map((t) => /([a-z0-9-]*clawbench-desktop-[a-z0-9-]*-payload)-/.exec(t)?.[1] ?? '')
      .filter((b) => b !== '')
  }

  it('publishes a payload for every non-macOS platform, and only those', () => {
    // Three desktop jobs build a payload; macOS deliberately does not (it would
    // break the .app code-signature seal).
    expect(payloadBasesInWorkflow().sort()).toEqual([
      'clawbench-desktop-linux-arm64-payload',
      'clawbench-desktop-linux-x64-payload',
      'clawbench-desktop-windows-x64-payload',
    ])
  })

  it('uses exactly the basenames the Go server advertises', () => {
    expect(payloadBasesInGo()).toEqual(payloadBasesInWorkflow().sort())
  })

  it('excludes macOS from the payload map', () => {
    // A darwin entry would make the client attempt an install that invalidates
    // the signature, and the failure would be silent (fallback to full).
    const go = readFileSync(repoPath('internal/service/desktop_upgrade.go'), 'utf8')
    const block = /desktopPayloadAssetBase\s*=\s*map\[string\]string\{([\s\S]*?)\n\}/.exec(go)?.[1] ?? ''
    expect(block).not.toContain('darwin')
  })
})

/**
 * The shell-fingerprint sidecar is what lets a payload install verify that the
 * shell it is reusing has the same identity (icon / appId / productName) it was
 * built against. Those live in the executable, not in `resources/`, so a
 * payload can never update them and nothing at runtime can notice a stale one.
 *
 * Two ordering constraints make this easy to get wrong, and both fail silently:
 *
 *   - the sidecar must be written BEFORE the full package is zipped, or it is
 *     not in the archive and every install lacks it;
 *   - the payload must be built BEFORE the full package is zipped (or its
 *     directory kept outside it), or the ~4MB payload is swept into the full
 *     archive.
 *
 * Both failures degrade to "every upgrade re-downloads ~150MB" rather than to
 * an error, so they are asserted structurally here.
 */
describe('shell fingerprint is wired into the desktop release jobs', () => {
  it('writes the fingerprint in every job that publishes a payload', () => {
    const all = jobs(releaseYml())
    for (const job of PAYLOAD_JOBS) {
      expect(all[job], `missing job ${job}`).toContain('shell-fingerprint.mjs')
    }
  })

  it('writes the fingerprint before zipping the full package', () => {
    // Ordering, not just presence: a sidecar written after the zip never
    // reaches the archive.
    const all = jobs(releaseYml())
    for (const job of PAYLOAD_JOBS) {
      const body = all[job]
      const fpAt = body.indexOf('shell-fingerprint.mjs')
      const zipAt = body.search(/Zip desktop|Compress-Archive.*-DestinationPath/)
      expect(fpAt, `${job}: no fingerprint step`).toBeGreaterThan(-1)
      expect(zipAt, `${job}: no packaging step`).toBeGreaterThan(-1)
      expect(fpAt, `${job}: fingerprint must be written before packaging`).toBeLessThan(zipAt)
    }
  })

  it('keeps the payload directory out of the full package', () => {
    // The payload dir is a sibling of the unpacked build precisely so the
    // full-package packaging step cannot sweep it up. If it ever moves back
    // inside, the full package silently gains a redundant payload copy.
    const all = jobs(releaseYml())
    for (const job of PAYLOAD_JOBS) {
      expect(all[job]).toContain('-payload')
    }
    // No packaging command may reference a nested `<unpacked>/payload` path.
    expect(releaseYml()).not.toMatch(/-Path\s+\S*unpacked\/payload\/\*/)
    expect(releaseYml()).not.toMatch(/cd\s+\S*unpacked\/payload\b/)
  })

  it('does not add a payload to macOS', () => {
    // macOS installs the full package only; a payload there would break the
    // code-signature seal. It therefore needs no fingerprint either.
    const all = jobs(releaseYml())
    expect(all['build-desktop-macos']).not.toContain('stage-payload.mjs')
  })
})

/**
 * The payload is also published to an npm registry mirror, so the client can
 * fetch ~1.2MB from npmmirror in mainland China instead of only from GitHub.
 * That adds a second naming contract across three places — the Go URL builder,
 * the npm package manifests, and the CI job that publishes them — and a
 * mismatch is silent: the npm candidate 404s, the client falls through to the
 * GitHub URL, and every upgrade quietly loses the mirror benefit.
 */
describe('npm payload packages match the Go URL derivation', () => {
  /** Package names declared by the checked-in payload manifests. */
  function payloadPackageNames(): string[] {
    const dir = repoPath('npm/desktop-payloads')
    return readdirSync(dir)
      .map((key) => JSON.parse(readFileSync(join(dir, key, 'package.json'), 'utf8')).name as string)
      .sort()
  }

  it('declares one package per non-macOS platform', () => {
    expect(payloadPackageNames()).toEqual([
      '@xulongzhe/clawbench-desktop-linux-arm64-payload',
      '@xulongzhe/clawbench-desktop-linux-x64-payload',
      '@xulongzhe/clawbench-desktop-windows-x64-payload',
    ])
  })

  it('derives those names from the release asset basenames, as Go does', () => {
    // Go builds the npm name as scope + asset base (desktopPayloadNpmPkg), and
    // the tarball URL from it. Pinning the relationship here means adding a
    // platform cannot leave the npm side behind.
    const go = readFileSync(repoPath('internal/service/desktop_upgrade.go'), 'utf8')
    expect(go).toMatch(/desktopNpmScope\s*\+\s*base/)

    const bases = payloadBasesInGo()
    const expected = bases.map((b) => `@xulongzhe/${b}`).sort()
    expect(payloadPackageNames()).toEqual(expected)
  })

  it('declares the manifest version placeholder, not a real release', () => {
    // CI rewrites the version from the tag. A checked-in real version would be
    // published as-is if that rewrite ever stopped running.
    const dir = repoPath('npm/desktop-payloads')
    for (const key of readdirSync(dir)) {
      const pkg = JSON.parse(readFileSync(join(dir, key, 'package.json'), 'utf8'))
      expect(pkg.version, `${key} version`).toBe('0.1.0')
      // Lifecycle scripts would run on every consumer's install.
      expect(pkg.scripts, `${key} must declare no scripts`).toBeUndefined()
    }
  })

  it('ships the manifest and the app resources', () => {
    // `files` is a whitelist: omitting payload.json or resources/ publishes an
    // empty package whose installs all fail the manifest check.
    const dir = repoPath('npm/desktop-payloads')
    for (const key of readdirSync(dir)) {
      const pkg = JSON.parse(readFileSync(join(dir, key, 'package.json'), 'utf8'))
      expect(pkg.files, `${key} files`).toContain('payload.json')
      expect(pkg.files, `${key} files`).toContain('resources/')
    }
  })

  it('uploads the payload artifacts with hidden files included', () => {
    // upload-artifact v4 skips dotfiles by default and the payload contains six.
    // Without this the npm package is silently incomplete — it installs fine and
    // only breaks at runtime.
    const all = jobs(releaseYml())
    for (const job of PAYLOAD_JOBS) {
      const body = all[job]
      const upload = body.slice(body.indexOf('upload-artifact'))
      expect(upload, `${job}: payload artifact upload must set include-hidden-files`).toContain(
        'include-hidden-files: true',
      )
      expect(upload, `${job}: payload dir must be uploaded`).toContain('-payload')
    }
  })

  it('publishes from a job that sets up the registry like publish-npm does', () => {
    // The repo .npmrc points at npmmirror and project config outranks
    // NPM_CONFIG_USERCONFIG, so without removing it the job publishes to the
    // mirror with no credentials.
    const all = jobs(releaseYml())
    const job = all['publish-npm-desktop']
    expect(job, 'missing publish-npm-desktop job').toBeTruthy()
    expect(job).toContain('rm -f .npmrc')
    expect(job).toContain('NPM_CONFIG_USERCONFIG')
    expect(job).toContain('npm publish')
    // A re-run must not republish an immutable version.
    expect(job).toContain('npm view')
    for (const dep of PAYLOAD_JOBS) {
      expect(job, `publish-npm-desktop must depend on ${dep}`).toContain(dep)
    }
  })
})

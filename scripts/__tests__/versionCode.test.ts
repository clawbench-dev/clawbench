import { describe, expect, it } from 'vitest'
import { execFileSync, spawnSync } from 'node:child_process'
import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

/**
 * Regression guard for the Android versionCode derivation.
 *
 * The bug: versionCode was `git rev-list --count HEAD`. actions/checkout
 * defaults to a depth=1 clone, so that collapses to 1 in CI, and every released
 * APK shipped versionCode=1 — verified against the real assets: the v0.95.0,
 * v0.98.0 and v0.99.0 GitHub Release APKs all report versionCode=1, while a dev
 * APK built locally from a full clone reported 1167.
 *
 * versionCode is the only field Android's installer compares, so the effect was
 * inverted: a newer release (1) could not replace an older dev build (1167), and
 * Android refused with "已安装更高版本".
 *
 * These tests exercise the REAL implementation — scripts/lib/version-code.sh is
 * executed, and the Groovy copy in android/app/build.gradle is executed by
 * Gradle itself. A hand-copied reimplementation would pass even if the shipped
 * code were broken.
 *
 * Bash/Gradle requirements: the shell tests need `bash` + `git`; the Gradle test
 * additionally needs a local Gradle distribution and is skipped without one.
 */

/** Locate a repo file, tolerating either cwd the suite may run from. */
function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const SCRIPT = repoPath('scripts/lib/version-code.sh')

/**
 * Run the real script. Uses spawnSync (not execFileSync) because several
 * assertions are about *stderr and exit status*, which execFileSync does not
 * expose reliably across Node versions.
 */
function runScript(args: string[]): { stdout: string; status: number; stderr: string } {
  const r = spawnSync('bash', [SCRIPT, ...args], { encoding: 'utf8' })
  return {
    stdout: (r.stdout ?? '').trim(),
    stderr: (r.stderr ?? '').trim(),
    status: r.status ?? 1,
  }
}

/** Run a command, returning its exit status and combined output. */
function run(cmd: string, args: string[], cwd?: string) {
  const r = spawnSync(cmd, args, { encoding: 'utf8', cwd })
  return { status: r.status ?? 1, out: `${r.stdout ?? ''}${r.stderr ?? ''}` }
}

/** Parse a `git describe --tags --long` string via the real implementation. */
function parse(describe: string): { code: number; status: number; stderr: string } {
  const r = runScript(['--parse', describe])
  return { code: Number(r.stdout), status: r.status, stderr: r.stderr }
}

/** Create a throwaway git repo with one commit, a tag, and N further commits. */
function makeRepo(tag: string, extraCommits: number): string {
  const dir = mkdtempSync(resolve(tmpdir(), 'vc-'))
  const git = (...args: string[]) => execFileSync('git', args, { cwd: dir, stdio: 'ignore' })
  git('init', '-q', '.')
  git('config', 'user.email', 't@t')
  git('config', 'user.name', 't')
  git('config', 'commit.gpgsign', 'false')
  git('config', 'tag.gpgsign', 'false')
  writeFileSync(resolve(dir, 'f'), 'x')
  git('add', 'f')
  git('commit', '-qm', 'init')
  git('tag', tag)
  for (let i = 0; i < extraCommits; i++) {
    writeFileSync(resolve(dir, 'f'), `x${i}\n`, { flag: 'a' })
    git('commit', '-qam', `c${i}`)
  }
  return dir
}

describe('version-code.sh — formula', () => {
  it('maps a release tag to a code that increases with the version', () => {
    // The exact table the implementation documents.
    expect(parse('v0.99.0-0-gfbc80b5').code).toBe(9_900_000)
    expect(parse('v0.100.0-0-gabc').code).toBe(10_000_000)
    expect(parse('v1.0.0-0-gabc').code).toBe(100_000_000)
    expect(parse('v0.97.0-0-gabc').code).toBe(9_700_000)
  })

  it('keeps builds between two releases ordered against both', () => {
    // This is why `distance` is part of the code: a dev build after v0.99.0
    // must outrank v0.99.0 yet still be outranked by v0.100.0.
    const atTag = parse('v0.99.0-0-gabc').code
    const dev = parse('v0.99.0-188-gabcdef1').code
    const next = parse('v0.100.0-0-gabc').code
    expect(atTag).toBeLessThan(dev)
    expect(dev).toBeLessThan(next)
  })

  it('does not collide v0.100.0 with v1.0.0', () => {
    // The whole reason for the wide minor field. Under the naive
    // `minor*10000` layout both collapse to 1000000, so v1.0.0 could never
    // replace v0.100.0.
    expect(parse('v0.100.0-0-gabc').code).not.toBe(parse('v1.0.0-0-gabc').code)
  })

  it('never lets a dev build of an older release outrank a newer release', () => {
    // Guards the field widths: with 3 digits for `distance` a dev build may
    // reach at most vX.Y.Z+999, which must stay below the next patch/minor.
    const worstDev = parse('v0.99.0-999-gabc').code
    expect(worstDev).toBeLessThan(parse('v0.99.1-0-gabc').code)
    expect(parse('v0.99.99-999-gabc').code).toBeLessThan(parse('v0.100.0-0-gabc').code)
  })
})

describe('version-code.sh — fail-open and clamping', () => {
  it('falls back to 1 for inputs that are not versioned builds', () => {
    // A bare hash, "dev", a pre-release tag, and an empty string all mean "no
    // comparable version" — never a mismatch prompt, never a blocked install.
    for (const input of ['a0f87a96', 'dev', 'v0.1.0-alpha-3-gabc', 'garbage']) {
      expect(parse(input).code, `input=${input}`).toBe(1)
    }
  })

  it('falls back to 1 when the checkout has no reachable version tag', () => {
    // The git path, not the --parse path: a repo with no tags at all. 1 is the
    // historical value, so nothing that used to be installable becomes
    // un-installable. Asserting the exact value matters — a fallback of 0 would
    // also be "not blocking", but it is a different, untested behaviour.
    const dir = mkdtempSync(resolve(tmpdir(), 'vc-notag-fallback-'))
    try {
      const git = (...args: string[]) => execFileSync('git', args, { cwd: dir, stdio: 'ignore' })
      git('init', '-q', '.')
      git('config', 'user.email', 't@t')
      git('config', 'user.name', 't')
      writeFileSync(resolve(dir, 'f'), 'x')
      git('add', 'f')
      git('commit', '-qm', 'init')

      const r = runScript(['--repo', dir])
      expect(r.status).toBe(0)
      expect(r.stdout).toBe('1')
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })

  it('clamps an over-long distance instead of failing', () => {
    // A gap over 999 only affects ordering among dev builds, so it must not
    // break the build. 5000 -> 999.
    const r = parse('v0.99.0-5000-gabc')
    expect(r.code).toBe(9_900_999)
    expect(r.stderr).toContain('clamped')
  })

  it('hard-fails a version outside the supported range', () => {
    // Silently truncating would ship a wrong, possibly lower, code. Exit 2 is
    // distinguishable from the fail-open 1.
    for (const [input, field] of [
      ['v21.0.0-0-gabc', 'major'],
      ['v0.1000.0-0-gabc', 'minor'],
      ['v0.0.100-0-gabc', 'patch'],
    ] as const) {
      const r = parse(input)
      expect(r.status, `input=${input}`).toBe(2)
      expect(r.stderr).toContain(field)
    }
  })
})

describe('version-code.sh — against a real checkout', () => {
  it('reads the nearest version tag, not the commit count', () => {
    // The regression in one assertion: in a depth=1 clone `git rev-list
    // --count HEAD` is 1, but the tag-derived code must not be.
    const dir = makeRepo('v0.42.0', 0)
    try {
      const shallow = mkdtempSync(resolve(tmpdir(), 'vc-shallow-'))
      try {
        // A depth=1 clone is exactly what actions/checkout produces.
        execFileSync('git', ['clone', '-q', '--depth', '1', `file://${dir}`, shallow], { stdio: 'ignore' })
        const r = runScript(['--repo', shallow])
        expect(r.status).toBe(0)
        expect(Number(r.stdout)).toBe(4_200_000)
        expect(Number(r.stdout)).not.toBe(1)
      } finally {
        rmSync(shallow, { recursive: true, force: true })
      }
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })

  it('asserts a release build is not degenerate', () => {
    const good = makeRepo('v0.99.0', 0)
    const noTags = mkdtempSync(resolve(tmpdir(), 'vc-notags-'))
    try {
      execFileSync('git', ['init', '-q', '.'], { cwd: noTags, stdio: 'ignore' })
      execFileSync('git', ['config', 'user.email', 't@t'], { cwd: noTags, stdio: 'ignore' })
      execFileSync('git', ['config', 'user.name', 't'], { cwd: noTags, stdio: 'ignore' })
      writeFileSync(resolve(noTags, 'f'), 'x')
      execFileSync('git', ['add', 'f'], { cwd: noTags, stdio: 'ignore' })
      execFileSync('git', ['commit', '-qm', 'init'], { cwd: noTags, stdio: 'ignore' })

      // A tagged checkout passes and prints a real code.
      const ok = runScript(['--repo', good, '--assert'])
      expect(ok.status).toBe(0)
      expect(Number(ok.stdout)).toBe(9_900_000)

      // A checkout with no reachable tag degenerates to 1 — --assert must turn
      // that into a failed release rather than an un-installable APK.
      const bad = runScript(['--repo', noTags, '--assert'])
      expect(bad.status).toBe(1)
      expect(bad.stderr).toContain('degenerate')
    } finally {
      rmSync(good, { recursive: true, force: true })
      rmSync(noTags, { recursive: true, force: true })
    }
  })
})

describe('versionCode derivation is not duplicated across the build', () => {
  const read = (rel: string) => readFileSync(repoPath(rel), 'utf8')

  /**
   * Strip comments before scanning for the banned expression.
   *
   * Every one of these files deliberately *mentions* `git rev-list --count HEAD`
   * in a comment explaining why it was removed. A naive substring scan flags
   * those explanations, which would make the guard impossible to satisfy
   * alongside the documentation it exists to preserve.
   */
  function stripComments(src: string): string {
    return src
      .split('\n')
      .filter((line) => !/^\s*(#|\/\/|\*|\/\*)/.test(line))
      .join('\n')
  }

  it('no longer derives the code from the commit count anywhere', () => {
    // `git rev-list --count HEAD` is the bug. It must not come back as live
    // code in any of the places that used it.
    for (const rel of [
      'build.sh',
      'android/app/build.gradle',
      '.github/workflows/ci.yml',
      '.github/workflows/release.yml',
    ]) {
      expect(stripComments(read(rel)), `${rel} must not use the commit count for versionCode`).not.toMatch(
        /git rev-list --count HEAD/i,
      )
    }
  })

  it('routes every versionCode producer through the shared script', () => {
    expect(read('build.sh')).toContain('scripts/lib/version-code.sh')
    expect(read('.github/workflows/ci.yml')).toContain('scripts/lib/version-code.sh')
    expect(read('.github/workflows/release.yml')).toContain('scripts/lib/version-code.sh')
  })

  it('asserts in the release path so a recurrence fails the release', () => {
    // The release workflow is the only place allowed to ship an APK to users,
    // so that is where the guard belongs.
    expect(read('.github/workflows/release.yml')).toMatch(/version-code\.sh"?\s+--assert/)
  })

  it('keeps the Gradle fallback on the same formula', () => {
    // The Gradle copy is a fallback, but it must not silently drift: assert it
    // uses the same tag-based source and the same multipliers.
    const gradle = stripComments(read('android/app/build.gradle'))
    expect(gradle).toContain('describe --tags --long')
    expect(gradle).toMatch(/100000000/)
    expect(gradle).toMatch(/100000/)
    expect(gradle).toMatch(/1000/)
    // ...and that the old commit-count derivation is gone.
    expect(gradle).not.toContain('rev-list --count')
  })
})

describe('Gradle autoVersionCode matches the shell implementation', () => {
  const gradleBin = findGradle()

  // Gradle takes ~1s per invocation and this test runs it once per case, so the
  // default 5s test timeout is not enough.
  const GRADLE_TIMEOUT_MS = 120_000

  it.skipIf(!gradleBin)(
    'produces the same code as the shell script for the same repo',
    () => {
      // Execute the real Groovy function via Gradle — the shell and the Gradle
      // fallback are two copies of one formula, and they must agree.
      const gradleSrc = readFileSync(repoPath('android/app/build.gradle'), 'utf8')
      const fn = extractGroovyFunction(gradleSrc, 'autoVersionCode')

      for (const [tag, extra, expected] of [
        ['v0.99.0', 0, 9_900_000],
        ['v0.100.0', 0, 10_000_000],
        ['v1.0.0', 0, 100_000_000],
        ['v0.99.0', 1200, 9_900_999], // distance clamped
      ] as const) {
        const dir = makeRepo(tag, extra)
        try {
          writeFileSync(resolve(dir, 'probe.gradle'), `${fn}\nprintln "VERSION_CODE=" + autoVersionCode()\n`)
          const r = run(gradleBin!, ['-q', '-b', 'probe.gradle', '--console=plain'], dir)
          const match = r.out.match(/VERSION_CODE=(\d+)/)
          expect(match, `no code in gradle output: ${r.out}`).not.toBeNull()

          // Both implementations, same repo.
          expect(Number(match![1]), `tag=${tag}+${extra}`).toBe(expected)
          expect(Number(runScript(['--repo', dir]).stdout)).toBe(expected)
        } finally {
          rmSync(dir, { recursive: true, force: true })
        }
      }
    },
    GRADLE_TIMEOUT_MS,
  )

  it.skipIf(!gradleBin)(
    'fails the build for a version outside the supported range',
    () => {
      const gradleSrc = readFileSync(repoPath('android/app/build.gradle'), 'utf8')
      const fn = extractGroovyFunction(gradleSrc, 'autoVersionCode')
      const dir = makeRepo('v21.0.0', 0)
      try {
        writeFileSync(resolve(dir, 'probe.gradle'), `${fn}\nprintln "VERSION_CODE=" + autoVersionCode()\n`)
        const r = run(gradleBin!, ['-q', '-b', 'probe.gradle', '--console=plain'], dir)
        expect(r.status).not.toBe(0)
        expect(r.out).toContain('major=21 exceeds')
      } finally {
        rmSync(dir, { recursive: true, force: true })
      }
    },
    GRADLE_TIMEOUT_MS,
  )
})

/** Locate a runnable Gradle: PATH first, then the wrapper cache. */
function findGradle(): string | null {
  try {
    execFileSync('gradle', ['--version'], { stdio: 'ignore' })
    return 'gradle'
  } catch {
    // fall through to the wrapper cache
  }
  const home = process.env.HOME ?? ''
  const cache = resolve(home, '.gradle/wrapper/dists')
  if (!existsSync(cache)) return null
  // ~/.gradle/wrapper/dists/gradle-<v>-bin/<hash>/gradle-<v>/bin/gradle
  // The cache also holds non-directories (CACHEDIR.TAG), hence the isDirectory
  // checks.
  for (const dist of readdirSync(cache)) {
    const distDir = resolve(cache, dist)
    if (!statSync(distDir).isDirectory()) continue
    const version = dist.replace(/-bin$/, '')
    for (const hash of readdirSync(distDir)) {
      const bin = resolve(distDir, hash, version, 'bin', 'gradle')
      if (existsSync(bin)) return bin
    }
  }
  return null
}

/** Pull `def <name>(...) { ... }` out of build.gradle by brace balancing. */
function extractGroovyFunction(source: string, name: string): string {
  const start = source.indexOf(`def ${name}(`)
  if (start < 0) throw new Error(`def ${name} not found in build.gradle`)
  let depth = 0
  for (let i = source.indexOf('{', start); i < source.length; i++) {
    if (source[i] === '{') depth++
    else if (source[i] === '}') {
      depth--
      if (depth === 0) return source.slice(start, i + 1)
    }
  }
  throw new Error(`unbalanced braces in def ${name}`)
}

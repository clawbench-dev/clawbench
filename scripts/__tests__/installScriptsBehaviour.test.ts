import { describe, expect, it } from 'vitest'
import { execFileSync, spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { delimiter, join, resolve } from 'node:path'

/**
 * Behaviour tests for the one-line installers (`scripts/install.sh` /
 * `install.ps1`).
 *
 * The sibling `installScripts.test.ts` only checks that certain strings are
 * present. That class of assertion passes happily while the installer is
 * broken: 「文案对但行为错」. These tests run the script instead — against a
 * sandboxed `$HOME` / `$CLAWBENCH_HOME` and stub executables on `PATH`, so no
 * network, no npm, and nothing outside a temp directory is touched.
 *
 * What is covered here (each maps to a real defect found in review):
 *   - `--help` / `--uninstall` actually do what they say, repeatedly
 *     (idempotent, and a 「装了一半」 machine still cleans up);
 *   - a `clawbench --version` that fails must surface its own stderr — the
 *     reason (missing platform package, missing .so) only ever exists there;
 *   - "latest LTS" must not depend on the order the JSON list comes back in;
 *   - every `2>/dev/null` in the script is deliberate, not a swallowed error.
 *
 * PowerShell: run only when `pwsh` exists. Without it the `--uninstall` /
 * `-Version` paths of install.ps1 cannot be exercised at all, so the tests are
 * skipped explicitly (never silently marked as passing).
 */

function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const INSTALL_SH = repoPath('scripts/install.sh')
const INSTALL_PS1 = repoPath('scripts/install.ps1')

function hasPwsh(): boolean {
  const r = spawnSync('pwsh', ['-NoLogo', '-NoProfile', '-Command', 'exit 0'], {
    stdio: 'ignore',
  })
  return r.status === 0
}
const PWSH = hasPwsh()

interface Sandbox {
  root: string
  home: string
  prefix: string
  /** Directory holding stub executables; put first on PATH. */
  bin: string
  cleanup(): void
}

/**
 * A throwaway HOME with stub binaries. `uname` is deliberately NOT stubbed —
 * the script reads the real one, and these tests run on Linux CI.
 */
function sandbox(stubs: Record<string, string> = {}): Sandbox {
  const root = mkdtempSync(join(tmpdir(), 'clawbench-install-test-'))
  const home = join(root, 'home')
  const prefix = join(root, 'clawbench-home')
  const bin = join(root, 'bin')
  mkdirSync(home, { recursive: true })
  mkdirSync(bin, { recursive: true })

  // `mktemp` in the script must not collide across parallel test files; keep
  // it on the real TMPDIR (it is fine) but make sure `PATH` still finds it.
  for (const [name, body] of Object.entries(stubs)) {
    const p = join(bin, name)
    writeFileSync(p, body, { mode: 0o755 })
  }

  return {
    root,
    home,
    prefix,
    bin,
    cleanup() {
      rmSync(root, { recursive: true, force: true })
    },
  }
}

interface RunResult {
  status: number | null
  stdout: string
  stderr: string
  output: string
}

function runScript(box: Sandbox, args: string[], env: Record<string, string> = {}): RunResult {
  // System paths stay on PATH so `bash`, `tr`, `sed`, `awk`, `mktemp`, `sort`
  // keep working; the sandbox bin comes first so stubs win.
  const path = [box.bin, '/usr/local/bin', '/usr/bin', '/bin'].join(delimiter)
  const r = spawnSync('bash', [INSTALL_SH, ...args], {
    env: {
      PATH: path,
      HOME: box.home,
      CLAWBENCH_HOME: box.prefix,
      SHELL: '/bin/bash',
      // keep output deterministic: no colour, not a tty anyway
      TERM: 'dumb',
      ...env,
    },
    encoding: 'utf8',
    timeout: 60_000,
  })
  const stdout = r.stdout ?? ''
  const stderr = r.stderr ?? ''
  return { status: r.status, stdout, stderr, output: stdout + stderr }
}

const FAKE_NPM_PASS = `#!/usr/bin/env bash
# Stub npm: records the invocation, then behaves like a successful global
# install. --uninstall tests never reach it.
printf '%s\\n' "$*" >> "$HOME/npm-args.log"
prefix=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --prefix) prefix="$2"; shift ;;
  esac
  shift
done
[ -n "$prefix" ] || exit 3
mkdir -p "$prefix/bin"
cat >"$prefix/bin/clawbench" <<'EOS'
#!/usr/bin/env bash
if [ -n "\${STUB_CLAWBENCH_FAIL:-}" ]; then
  echo "Error: Cannot find module '@xulongzhe/clawbench-linux-x64/bin/clawbench-linux-x64'" >&2
  echo "node:internal/modules/cjs/loader:1147" >&2
  exit 1
fi
echo "clawbench \${STUB_CLAWBENCH_VERSION:-0.130.0}"
EOS
chmod +x "$prefix/bin/clawbench"
exit 0
`

/**
 * Stubs that make the install path reach the npm step without downloading
 * anything: `curl` answers the mirror ping, a pre-existing Node is picked up
 * from the sandbox bin.
 */
function installStubs(): Record<string, string> {
  return {
    // `node -v` must look >= 20 so the installer reuses it instead of
    // downloading a tarball.
    node: '#!/usr/bin/env bash\necho v22.11.0\n',
    npm: FAKE_NPM_PASS,
    // curl: --ping endpoint answers 200; everything else is unreachable.
    // The script tests reachability with `curl -fsS -m 8 -o /dev/null`.
    curl: `#!/usr/bin/env bash
args="$*"
out="/dev/null"
prev=""
for a in "$@"; do [ "$prev" = "-o" ] && out="$a"; prev="$a"; done
case "$args" in
  *"/-/ping"*) exit 0 ;;
esac
# Any other request: offline. This proves the failing path too.
printf 'curl: (7) Failed to connect\\n' >&2
exit 7
`,
    wget: '#!/usr/bin/env bash\nexit 1\n',
  }
}

describe('install.sh: --help', () => {
  it('prints usage and exits 0 without touching anything', () => {
    const box = sandbox()
    try {
      const r = runScript(box, ['--help'])
      expect(r.status).toBe(0)
      expect(r.output).toContain('用法')
      expect(r.output).toContain('--uninstall')
      expect(r.output).toContain('CLAWBENCH_VERSION')
      // --help must not run the installer
      expect(existsSync(box.prefix)).toBe(false)
    } finally {
      box.cleanup()
    }
  })

  it('rejects an unknown flag with a non-zero exit and a usable message', () => {
    const box = sandbox()
    try {
      const r = runScript(box, ['--frobnicate'])
      expect(r.status).not.toBe(0)
      expect(r.output).toContain('--frobnicate')
      expect(r.output).toContain('--help')
      expect(existsSync(box.prefix)).toBe(false)
    } finally {
      box.cleanup()
    }
  })
})

describe('install.sh: --uninstall', () => {
  function seedInstall(box: Sandbox, rc: string) {
    // The three pieces of state an install writes:
    //   1. the install directory (node + npm prefix)
    //   2. a PATH marker block in the shell rc file
    //   3. (npm only) ~/.npm/_lib link — checked separately below
    mkdirSync(join(box.prefix, 'node', 'bin'), { recursive: true })
    mkdirSync(join(box.prefix, 'npm', 'bin'), { recursive: true })
    writeFileSync(join(box.prefix, 'node', 'bin', 'node'), '#!/bin/sh\n', { mode: 0o755 })
    const rcPath = join(box.home, rc)
    writeFileSync(
      rcPath,
      [
        '# user content before',
        'export EDITOR=vim',
        '',
        '# >>> clawbench installer >>>',
        '# ClawBench / Node.js 的安装目录（由 install.sh 添加）',
        `export PATH="${box.prefix}/node/bin:${box.prefix}/npm/bin:$PATH"`,
        '# <<< clawbench installer <<<',
        '',
        '# user content after',
        '',
      ].join('\n'),
    )
    return rcPath
  }

  it('removes the install dir and its rc marker, leaving the rest of the rc intact', () => {
    const box = sandbox()
    try {
      const rcPath = seedInstall(box, '.bashrc')
      const before = readFileSync(rcPath, 'utf8')

      const r = runScript(box, ['--uninstall'])
      expect(r.status).toBe(0)
      expect(existsSync(box.prefix)).toBe(false)

      const after = readFileSync(rcPath, 'utf8')
      expect(after).not.toContain('clawbench installer')
      expect(after).not.toContain(box.prefix)
      // Everything the user wrote stays, in order.
      expect(after).toContain('export EDITOR=vim')
      expect(after).toContain('# user content before')
      expect(after).toContain('# user content after')
      // Every user line is still there, in order, with the marker block gone.
      // Asserted as a sequence (not a line count, and not a byte-exact file)
      // because the blank line that preceded the block is left behind — an
      // empty line in a shell rc is harmless, whereas a "tidy" version that
      // also drops neighbouring lines is how a config gets damaged.
      expect(
        after
          .split('\n')
          .filter((l) => l !== ''),
      ).toEqual(['# user content before', 'export EDITOR=vim', '# user content after'])
      expect(before).not.toBe(after)
    } finally {
      box.cleanup()
    }
  })

  it('is idempotent: a second --uninstall still succeeds and touches nothing', () => {
    const box = sandbox()
    try {
      const rcPath = seedInstall(box, '.bashrc')
      expect(runScript(box, ['--uninstall']).status).toBe(0)
      const afterFirst = readFileSync(rcPath, 'utf8')

      const second = runScript(box, ['--uninstall'])
      expect(second.status).toBe(0)
      expect(second.output).toContain('跳过')
      expect(readFileSync(rcPath, 'utf8')).toBe(afterFirst)
    } finally {
      box.cleanup()
    }
  })

  it('cleans up a half-install: directory exists but no rc marker was ever written', () => {
    // The realistic abort: rc was unwritable (read-only home), so only the
    // install directory exists. Uninstall must still remove it and not fail.
    const box = sandbox()
    try {
      mkdirSync(join(box.prefix, 'npm'), { recursive: true })
      const rcPath = join(box.home, '.bashrc')
      writeFileSync(rcPath, 'export EDITOR=vim\n')

      const r = runScript(box, ['--uninstall'])
      expect(r.status).toBe(0)
      expect(existsSync(box.prefix)).toBe(false)
      expect(readFileSync(rcPath, 'utf8')).toBe('export EDITOR=vim\n')
    } finally {
      box.cleanup()
    }
  })

  it('drops the npm ~/.npm/_lib link for the package', () => {
    const box = sandbox()
    try {
      const link = join(box.home, '.npm', '_lib', 'node_modules', '@xulongzhe', 'clawbench')
      mkdirSync(join(link, '..'), { recursive: true })
      mkdirSync(join(box.root, 'real'), { recursive: true })
      execFileSync('ln', ['-s', join(box.root, 'real'), link])

      const r = runScript(box, ['--uninstall'])
      expect(r.status).toBe(0)
      expect(existsSync(link)).toBe(false)
      // The real directory the link pointed at is untouched.
      expect(statSync(join(box.root, 'real')).isDirectory()).toBe(true)
    } finally {
      box.cleanup()
    }
  })

  it('does not let a dangling start marker eat the rest of the rc file', () => {
    // A hand-edited rc can keep `# >>> clawbench installer >>>` while losing the
    // end marker. A naive awk "delete between markers" then drops EVERYTHING
    // below it — the user's whole shell config. Only the marker line itself
    // may go; the rest has to survive.
    const box = sandbox()
    try {
      const rcPath = join(box.home, '.bashrc')
      writeFileSync(rcPath, '# >>> clawbench installer >>>\nexport EDITOR=vim\n')
      const r = runScript(box, ['--uninstall'])
      expect(r.status, r.output).toBe(0)
      const after = readFileSync(rcPath, 'utf8')
      expect(after).not.toContain('>>> clawbench installer >>>')
      expect(after).toContain('export EDITOR=vim')
    } finally {
      box.cleanup()
    }
  })
})

describe('install.sh: install path', () => {
  it('installs latest by default and prints the version the binary reports', () => {
    const box = sandbox(installStubs())
    try {
      const r = runScript(box, [], { STUB_CLAWBENCH_VERSION: '0.130.0' })
      expect(r.status).toBe(0)

      const args = readFileSync(join(box.home, 'npm-args.log'), 'utf8')
      expect(args).toContain('install -g @xulongzhe/clawbench@latest')
      expect(args).toContain(`--prefix ${box.prefix}/npm`)
      expect(r.output).toContain('clawbench 0.130.0')
      // PATH marker written once, and says how to undo the install.
      const rc = readFileSync(join(box.home, '.bashrc'), 'utf8')
      expect(rc).toContain('>>> clawbench installer >>>')
      expect(r.output).toContain('--uninstall')
    } finally {
      box.cleanup()
    }
  })

  it('pins the version with CLAWBENCH_VERSION and reports what was actually installed', () => {
    const box = sandbox(installStubs())
    try {
      const r = runScript(box, [], {
        CLAWBENCH_VERSION: '0.129.4',
        // A registry can resolve a pinned range to a different build; the
        // installer must report the binary's own answer, not the request.
        STUB_CLAWBENCH_VERSION: '0.129.4',
      })
      expect(r.status).toBe(0)
      const args = readFileSync(join(box.home, 'npm-args.log'), 'utf8')
      expect(args).toContain('@xulongzhe/clawbench@0.129.4')
      expect(args).not.toContain('@latest')
      expect(r.output).toContain('clawbench 0.129.4')
    } finally {
      box.cleanup()
    }
  })

  it('surfaces the real stderr when clawbench --version fails', () => {
    // The defect this pins: `clawbench --version 2>/dev/null` turned "the
    // platform package is missing" into a generic "it does not start" with the
    // one line that identifies the cause thrown away.
    const box = sandbox(installStubs())
    try {
      const r = runScript(box, [], { STUB_CLAWBENCH_FAIL: '1' })
      expect(r.status).not.toBe(0)
      expect(r.output).toContain('Cannot find module')
      expect(r.output).toContain('clawbench-linux-x64')
      // and it must still say what to do next
      expect(r.output).toContain('EBADPLATFORM')
    } finally {
      box.cleanup()
    }
  })

  it('warns and prints the manual PATH line when the rc file is unwritable', () => {
    // Read-only home (or a read-only rc) used to make `>> "$RC"` fail and
    // `set -e` abort *before* telling the user the install had succeeded.
    const box = sandbox(installStubs())
    try {
      const rcPath = join(box.home, '.bashrc')
      writeFileSync(rcPath, 'export EDITOR=vim\n', { mode: 0o444 })

      const r = runScript(box, [])
      expect(r.status, r.output).toBe(0)
      expect(r.output).toContain('警告')
      // The rc got no node dir in it (there is no bundled node here), so the
      // line the user is told to paste is exactly the install's npm dir.
      expect(r.output).toContain(`export PATH="${box.prefix}/npm/bin:$PATH"`)
      expect(r.output).toContain('装好了')
      // The read-only file was not modified.
      expect(readFileSync(rcPath, 'utf8')).toBe('export EDITOR=vim\n')
    } finally {
      box.cleanup()
    }
  })

  it('picks the newest LTS regardless of the order the list arrives in', () => {
    // The old code took the FIRST entry carrying an `lts` field. API order is
    // not a contract, so a reordered list silently installed an older major.
    // Driven end-to-end: curl serves an index.json whose newest entry is LAST,
    // and the installer must name the highest LTS in the download URL — which
    // here is v22 (v24 is New, not yet an LTS line, and has `lts: false`).
    const box = sandbox({
      ...installStubs(),
      // An OLD Node is what makes it download. `npm` is required too, or the
      // script dies before it ever asks for the index.
      node: '#!/usr/bin/env bash\n[ "$1" = "-v" ] && echo v18.0.0\nexit 0\n',
      npm: '#!/usr/bin/env bash\nexit 0\n',
      // Reads the fixture from a file so the JSON bytes are exactly what a
      // registry returns (no shell quoting or heredoc in the way).
      curl: `#!/usr/bin/env bash
args="$*"
case "$args" in
  *"/-/ping"*) exit 0 ;;
  *"index.json"*) /bin/cat "$FIXTURE"; exit 0 ;;
  *".tar.gz"*)
    # Recorded before anything is extracted: this test only cares which
    # version the installer asked for.
    echo "$args" > "$HOME/requested-url.txt"
    printf 'curl: (6) offline\\n' >&2
    exit 6 ;;
  *"SHASUMS256.txt"*) exit 6 ;;
esac
printf 'curl: (7) offline\\n' >&2
exit 7
`,
    })
    try {
      const fixture = join(box.root, 'index.json')
      writeFileSync(
        fixture,
        [
          '[',
          '{"version":"v24.10.0","lts":false},',
          '{"version":"v22.20.0","lts":"Jod"},',
          '{"version":"v22.9.0","lts":"Jod"},',
          '{"version":"v20.18.0","lts":"Iron"},',
          '{"version":"v18.20.9","lts":"Hydrogen"}',
          ']',
        ].join('\n'),
      )

      const r = runScript(box, [], { FIXTURE: fixture })
      // The download fails (we stubbed it), but the URL shows the choice.
      expect(r.status, r.output).not.toBe(0)
      const url = (() => {
        try {
          return readFileSync(join(box.home, 'requested-url.txt'), 'utf8')
        } catch {
          throw new Error(`no tarball was requested; installer output:\n${r.output}`)
        }
      })()
      // v24 comes first in the list but is not LTS; v22.20 beats v22.9.
      // String-comparing versions would pick 22.9 (9 > 2).
      // The `v` prefix in BOTH the directory and the file name is load-bearing:
      // both nodejs.org and npmmirror publish `v22.20.0/node-v22.20.0-…`, and
      // dropping it makes every request 404 (`node^v` would accept the broken
      // form, which is exactly how that bug shipped).
      expect(url, `installer output:\n${r.output}`).toMatch(/\/v22\.20\.0\/node-v22\.20\.0-/)
      expect(url).not.toMatch(/node-v?24\./)
      expect(url).not.toMatch(/node-v?22\.9\.0-/)
    } finally {
      box.cleanup()
    }
  })
})

describe('install.sh: fail before wasting the download', () => {
  /**
   * A minimal image (container, server install) has no libatomic1. The old
   * behaviour downloaded ~60MB, extracted, and only then reported
   * "error while loading shared libraries" — on a slow link that is minutes
   * wasted for a fact knowable up front.
   *
   * The check must look for the library itself, not probe the system `node`:
   * distro builds of node are often patched and do not need libatomic.so.1,
   * so a working `node` says nothing about the official binary we download.
   */
  it('stops before downloading when libatomic.so.1 is absent', () => {
    const box = sandbox({
      // A system node that runs fine — exactly the case the old probe let
      // through. The library is what decides.
      node: '#!/usr/bin/env bash\n[ "$1" = "-v" ] && echo v18.0.0\nexit 0\n',
      npm: '#!/usr/bin/env bash\nexit 0\n',
      curl: `#!/usr/bin/env bash
args="$*"
echo "$args" >> "$HOME/curl-args.log"
case "$args" in
  *"/-/ping"*) exit 0 ;;
esac
exit 7
`,
      // The host has libatomic.so.1, so "the library is missing" can only be
      // tested by pointing the lookup somewhere empty — the sandbox root has
      // no libatomic.so.1 in it.
      ldconfig: '#!/usr/bin/env bash\nexit 0\n',
    })
    try {
      const r = runScript(box, [], { CLAWBENCH_LIBDIR_OVERRIDE: box.root })
      expect(r.status, r.output).not.toBe(0)
      // Actionable: the user must be told what to install, not shown the .so error alone.
      expect(r.output).toContain('libatomic1')
      expect(r.output).toContain('apt-get install')
      // And nothing may have been downloaded.
      const asked = existsSync(join(box.home, 'curl-args.log'))
        ? readFileSync(join(box.home, 'curl-args.log'), 'utf8')
        : ''
      expect(asked, `installer output:\n${r.output}`).not.toContain('.tar.gz')
      expect(asked).not.toContain('index.json')
    } finally {
      box.cleanup()
    }
  })

  it('says tar is missing instead of surfacing a confusing download error', () => {
    const box = sandbox({
      npm: '#!/usr/bin/env bash\nexit 0\n',
      // The tar check comes first, so the library answer is irrelevant here —
      // but it must not be the reason this test fails.
      ldconfig: '#!/usr/bin/env bash\necho " libatomic.so.1 (libc6,x86-64) => /lib/x86_64-linux-gnu/libatomic.so.1"\n',
      curl: `#!/usr/bin/env bash
args="$*"
case "$args" in
  *"/-/ping"*) exit 0 ;;
  *"index.json"*) printf '[{"version":"v22.20.0","lts":"Jod"}]'; exit 0 ;;
esac
exit 7
`,
    })
    try {
      // A PATH that has every program the script needs EXCEPT `tar` — the
      // minimal-image case. Preparing it by symlink keeps the real binaries
      // (mktemp, sed, awk…) available so the script reaches the check instead
      // of dying on something unrelated first.
      const tools = join(box.root, 'tools')
      mkdirSync(tools, { recursive: true })
      for (const name of ['bash', 'sh', 'uname', 'mktemp', 'tr', 'sed', 'awk', 'sort', 'cat', 'rm', 'mkdir', 'mv', 'ln', 'dirname', 'grep', 'id']) {
        const real = ['/usr/bin', '/bin'].map((d) => join(d, name)).find((p) => existsSync(p))
        if (real) spawnSync('ln', ['-sf', real, join(tools, name)])
      }
      expect(existsSync(join(tools, 'tar')), 'the sandbox must not contain tar').toBe(false)

      const r = spawnSync(join(tools, 'bash'), [INSTALL_SH], {
        env: {
          PATH: [tools, box.bin].join(delimiter),
          HOME: box.home,
          CLAWBENCH_HOME: box.prefix,
          SHELL: '/bin/bash',
          TERM: 'dumb',
        },
        encoding: 'utf8',
        timeout: 60_000,
      })
      const output = `${r.stdout ?? ''}${r.stderr ?? ''}`
      expect(r.status, output).not.toBe(0)
      // Names the missing program and how to get it, rather than letting the
      // user read `tar: not found` out of the middle of a download log.
      expect(output).toContain('需要 tar')
      expect(output).toContain('apt-get install')
      expect(output).not.toContain('下载 Node.js 失败')
    } finally {
      box.cleanup()
    }
  })
})

describe('install.sh: checksum sourcing', () => {
  /**
   * The download succeeded, so the installer must not make the user wait on a
   * second, much slower source just to get SHASUMS256.txt. Pinning it here:
   * `SHASUMS256.txt` must be asked of the mirror FIRST even when the mirror
   * answered `/index.json` and the tarball.
   */
  it('asks the mirror for SHASUMS256.txt before falling back to the official site', () => {
    const box = sandbox({
      node: '#!/usr/bin/env bash\n[ "$1" = "-v" ] && echo v18.0.0\nexit 0\n',
      npm: '#!/usr/bin/env bash\nexit 0\n',
      curl: `#!/usr/bin/env bash
args="$*"
echo "$args" >> "$HOME/curl-args.log"
case "$args" in
  *"/-/ping"*) exit 0 ;;
  *"index.json"*)
    printf '[{"version":"v22.20.0","lts":"Jod"}]'
    exit 0 ;;
  *"SHASUMS256.txt"*)
    # Deliberately unparseable so no sha256sum of a stub tarball is attempted;
    # this test only cares WHICH host was asked, and in what order.
    exit 6 ;;
  *".tar.gz"*)
    # A successful download: the checksum step only runs after one.
    printf 'not a tarball' > "\${out:-/dev/null}"
    exit 0 ;;
esac
exit 7
`,
    })
    try {
      const r = runScript(box, [])
      expect(r.status, r.output).not.toBe(0)
      const asked = readFileSync(join(box.home, 'curl-args.log'), 'utf8')
        .split('\n')
        .filter((l) => l.includes('SHASUMS256.txt'))
      expect(asked.length, `installer output:\n${r.output}`).toBeGreaterThan(0)
      expect(asked[0]).toContain('registry.npmmirror.com')
      expect(asked[0]).toContain('/v22.20.0/')
    } finally {
      box.cleanup()
    }
  })
})

describe('install.sh: swallowed stderr is deliberate', () => {
  // A blanket `2>/dev/null` can hide the one line a user needs (that is
  // exactly what happened to `clawbench --version`). Each remaining one must
  // sit on a probe whose failure is handled by the caller, and carry a nearby
  // comment saying why. This test makes adding a new one without a reason fail.
  it('keeps every 2>/dev/null on a commented probe, never on the version check', () => {
    const src = readFileSync(INSTALL_SH, 'utf8')
    const lines = src.split('\n')
    const offenders: string[] = []
    lines.forEach((line, i) => {
      if (!line.includes('2>/dev/null')) return
      if (/clawbench --version/.test(line)) {
        offenders.push(`line ${i + 1} swallows the version check's stderr: ${line.trim()}`)
        return
      }
      // Allowed shapes: `command -v foo >/dev/null 2>&1` is different, but a
      // literal 2>/dev/null must have a comment within 12 lines above.
      const context = lines.slice(Math.max(0, i - 12), i).join('\n')
      if (!context.includes('#') && !context.includes('//')) {
        offenders.push(`line ${i + 1} has no explanatory comment: ${line.trim()}`)
      }
    })
    expect(offenders).toEqual([])
  })

  it('the version check captures stdout AND stderr', () => {
    const src = readFileSync(INSTALL_SH, 'utf8')
    expect(src).toMatch(/if ! version="\$\(clawbench --version 2>&1\)"/)
  })
})

describe('install.ps1', () => {
  // Nothing here parses or runs .ps1 without PowerShell, so a machine without
  // `pwsh` gets an explicit skip rather than a green run that tested nothing.
  const psSuite = PWSH ? describe : describe.skip

  psSuite('（需要 pwsh，本机未安装则整组 skip）', () => {
    /** Runs install.ps1 against a throwaway CLAWBENCH_HOME. */
    function runPs1(args: string[], home: string) {
      const r = spawnSync(
        'pwsh',
        ['-NoLogo', '-NoProfile', '-NonInteractive', '-File', INSTALL_PS1, ...args],
        {
          env: { ...process.env, CLAWBENCH_HOME: join(home, 'install-root') },
          encoding: 'utf8',
          timeout: 60_000,
        },
      )
      return { status: r.status, output: `${r.stdout ?? ''}${r.stderr ?? ''}` }
    }

    it('parses (a syntax error only surfaces when a stranger pipes it)', () => {
      const r = spawnSync(
        'pwsh',
        [
          '-NoLogo',
          '-NoProfile',
          '-NonInteractive',
          '-Command',
          `$errors = $null; [System.Management.Automation.Language.Parser]::ParseFile('${INSTALL_PS1.replace(/'/g, "''")}', [ref]$null, [ref]$errors) | Out-Null; if ($errors.Count -gt 0) { $errors | ForEach-Object { Write-Output $_.Message }; exit 1 }`,
        ],
        { encoding: 'utf8' },
      )
      expect((r.stdout ?? '').trim(), r.stderr).toBe('')
      expect(r.status).toBe(0)
    })

    it('-Help documents -Uninstall and exits without touching the install dir', () => {
      const home = mkdtempSync(join(tmpdir(), 'clawbench-ps-help-'))
      try {
        const { status, output } = runPs1(['-Help'], home)
        expect(status, output).toBe(0)
        expect(output).toContain('-Uninstall')
        expect(output).toContain('CLAWBENCH_VERSION')
        expect(existsSync(join(home, 'install-root'))).toBe(false)
      } finally {
        rmSync(home, { recursive: true, force: true })
      }
    })

    it('-Uninstall removes the install dir, and running it twice is safe', () => {
      // The three pieces of state an install writes: the directory, the two
      // user-PATH entries, and nothing else. Only the directory is assertable
      // cross-platform (the "User" PATH is the registry on Windows); the
      // idempotency requirement is the same on both hosts.
      const home = mkdtempSync(join(tmpdir(), 'clawbench-ps-uni-'))
      try {
        const root = join(home, 'install-root')
        mkdirSync(join(root, 'node'), { recursive: true })
        mkdirSync(join(root, 'npm'), { recursive: true })
        writeFileSync(join(root, 'npm', 'clawbench.cmd'), '@echo off\n')

        const first = runPs1(['-Uninstall'], home)
        expect(first.status, first.output).toBe(0)
        expect(existsSync(root)).toBe(false)

        const second = runPs1(['-Uninstall'], home)
        expect(second.status, second.output).toBe(0)
        expect(second.output).toContain('跳过')
      } finally {
        rmSync(home, { recursive: true, force: true })
      }
    })

    it('pins the npm spec when -Version is given', () => {
      // Static only: the install path needs network + npm. It pins the two
      // lines that decide the spec — the rest is covered by install.sh's
      // equivalent behaviour test.
      const src = readFileSync(INSTALL_PS1, 'utf8')
      expect(src).toMatch(/\[string\]\$Version/)
      expect(src).toMatch(/\$FixedVersion\s*=\s*if\s*\(\$Version\)\s*\{\s*\$Version/)
      expect(src).toMatch(/\$npmSpec\s*=\s*"\$NpmPackage@\$FixedVersion"/)
    })
  })

  if (!PWSH) {
    it('the PowerShell behaviour suite is skipped because pwsh is not installed', () => {
      // Explicit, and named in the report: on a machine without `pwsh` nothing
      // above ran, so a green run is NOT evidence that install.ps1 works.
      console.warn('[install.ps1] pwsh not found — behaviour tests skipped (needs `pwsh` on PATH)')
      expect(PWSH).toBe(false)
    })
  }
})

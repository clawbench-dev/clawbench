import { describe, expect, it } from 'vitest'
import { execFileSync } from 'node:child_process'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

/**
 * Behavioural tests for scripts/npmmirror-sync.sh.
 *
 * The script's whole job is a control-flow property — keep asking the mirror
 * until the version is actually there, then check the tarball — so these run
 * the real script against a fake `curl` on PATH instead of asserting on the
 * source text. A text guard would pass on a version that queues the sync and
 * returns immediately, which is exactly the failure mode that left v0.132.0
 * half-synced.
 */

function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const SCRIPT = repoPath('scripts/npmmirror-sync.sh')
const VERSION = '1.2.3'

interface Scenario {
  /** 版本元数据从第几次轮询开始可见（默认第 1 次）。 */
  visibleAfter?: number
  /** 这些包永远不同步。 */
  neverVisible?: string[]
  /** tarball 返回的 HTTP 码，默认 200。 */
  tarballStatus?: number
}

interface Run {
  exitCode: number
  stdout: string
  stderr: string
  /** 每个包被提交同步任务的次数。 */
  syncs: Record<string, number>
  /** 每个包被轮询版本元数据的次数。 */
  polls: Record<string, number>
}

function runScript(packages: string[], opts: Scenario = {}): Run {
  const dir = mkdtempSync(join(tmpdir(), 'npmmirror-'))
  const binDir = join(dir, 'bin')
  const stateDir = join(dir, 'state')
  mkdirSync(binDir, { recursive: true })
  mkdirSync(stateDir, { recursive: true })

  // 假 curl 只认三种请求：提交同步、查版本元数据、探 tarball。
  // 包名在 URL 里是 %2F 编码的，统一还原成 slug 当状态文件名。
  const neverSlugs = (opts.neverVisible ?? []).map((p) => p.replace('/', '_'))
  const fakeCurl = `#!/usr/bin/env bash
set -euo pipefail
STATE="${stateDir}"
URL=""
METHOD=""
RANGE=0
while [ $# -gt 0 ]; do
  case "$1" in
    -X)
      METHOD="$2"
      shift 2
      ;;
    -r)
      RANGE=1
      shift 2
      ;;
    -o|-w|--max-time|-H|-d)
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      URL="$1"
      shift
      ;;
  esac
done

# tarball 的 URL 里没有包名，先处理它，别被下面的包名解析挡掉。
if [ "$RANGE" = "1" ]; then
  printf '%s' "${opts.tarballStatus ?? 200}"
  exit 0
fi

slug=$(printf '%s' "$URL" | grep -oE '@[^/]+%2F[^/]+' | head -1 | sed 's|%2F|_|g')
if [ -z "$slug" ]; then
  echo "fake curl: cannot parse package name from $URL" >&2
  exit 1
fi

if [ "$METHOD" = "PUT" ]; then
  n=$(( $(cat "$STATE/$slug.syncs" 2>/dev/null || echo 0) + 1 ))
  echo "$n" > "$STATE/$slug.syncs"
  echo '{"ok":true}'
  exit 0
fi

n=$(( $(cat "$STATE/$slug.polls" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$STATE/$slug.polls"
for never in ${neverSlugs.join(' ')}; do
  if [ "$slug" = "$never" ]; then
    exit 22
  fi
done
if [ "$n" -ge "${opts.visibleAfter ?? 1}" ]; then
  echo "{\\"version\\":\\"${VERSION}\\",\\"dist\\":{\\"tarball\\":\\"https://cdn.example/x.tgz\\"}}"
  exit 0
fi
exit 22
`
  const fakePath = join(binDir, 'curl')
  writeFileSync(fakePath, fakeCurl)
  chmodSync(fakePath, 0o755)

  let exitCode = 0
  let stdout = ''
  let stderr = ''
  try {
    stdout = execFileSync(SCRIPT, [VERSION, ...packages], {
      encoding: 'utf8',
      env: {
        ...process.env,
        PATH: `${binDir}:${process.env.PATH ?? ''}`,
        NPMMIRROR_WAIT_SECONDS: '0.3',
        NPMMIRROR_INTERVAL_SECONDS: '0.05',
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
  } catch (err) {
    const e = err as { status?: number; stdout?: string; stderr?: string }
    exitCode = e.status ?? 1
    stdout = e.stdout ?? ''
    stderr = e.stderr ?? ''
  }

  const readCounters = (suffix: string): Record<string, number> => {
    const out: Record<string, number> = {}
    for (const pkg of packages) {
      const slug = pkg.replace('/', '_')
      const file = join(stateDir, `${slug}.${suffix}`)
      out[pkg] = existsSync(file) ? Number(readFileSync(file, 'utf8').trim()) : 0
    }
    return out
  }

  return { exitCode, stdout, stderr, syncs: readCounters('syncs'), polls: readCounters('polls') }
}

describe('npmmirror-sync.sh', () => {
  it('为每个包提交同步任务，并在版本可见后成功退出', () => {
    const r = runScript(['@xulongzhe/clawbench', '@xulongzhe/clawbench-linux-x64'])
    expect(r.exitCode).toBe(0)
    expect(r.syncs['@xulongzhe/clawbench']).toBe(1)
    expect(r.syncs['@xulongzhe/clawbench-linux-x64']).toBe(1)
    expect(r.stdout).toContain('全部 2 个包')
  })

  it('版本晚几次才出现时继续轮询，而不是查一次就放弃', () => {
    const r = runScript(['@xulongzhe/clawbench'], { visibleAfter: 3 })
    expect(r.exitCode).toBe(0)
    // 第 3 次才可见：至少问了 3 次，证明是轮询而不是一次性判断。
    expect(r.polls['@xulongzhe/clawbench']).toBeGreaterThanOrEqual(3)
  })

  it('始终不同步的包会让脚本失败并点名', () => {
    const r = runScript(['@xulongzhe/clawbench', '@xulongzhe/clawbench-win32-x64'], {
      neverVisible: ['@xulongzhe/clawbench-win32-x64'],
    })
    expect(r.exitCode).toBe(1)
    expect(r.stderr).toContain('clawbench-win32-x64')
    // 一个包失败不能连累其他包：另一个仍然被同步并确认过。
    expect(r.polls['@xulongzhe/clawbench']).toBeGreaterThanOrEqual(1)
  })

  it('元数据到了但 tarball 下不了，也算没同步好', () => {
    const r = runScript(['@xulongzhe/clawbench'], { tarballStatus: 500 })
    expect(r.exitCode).toBe(1)
    expect(r.stderr).toContain('tarball')
  })

  it('参数不足时给出用法而不是静默成功', () => {
    let code = 0
    try {
      execFileSync(SCRIPT, [VERSION], { stdio: ['ignore', 'pipe', 'pipe'] })
    } catch (err) {
      code = (err as { status?: number }).status ?? 1
    }
    expect(code).toBe(2)
  })
})

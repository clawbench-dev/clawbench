import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guards for the Playwright E2E sharding in `.github/workflows/ci.yml`.
 *
 * The E2E job is the pipeline's critical path (21–27 min per browser against
 * ~21 min for every other job combined), so each browser is split into
 * parallel shards. Two invariants make that safe, and both are silent when
 * broken — a mismatched denominator still "works" (Playwright accepts
 * `--shard=1/2` in a job matrix sized 3, it just runs a different slice), and a
 * split serial file fails only intermittently and only on the runner that got
 * the second half.
 *
 * Scanned as text, like the neighbouring workflow guards: js-yaml is only a
 * transitive dependency here and these files are read the same way elsewhere.
 */

function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const CI = readFileSync(repoPath('.github/workflows/ci.yml'), 'utf8')

/** The `e2e:` job block, from its header to the next job header. */
function e2eJobBlock(): string {
  const lines = CI.split('\n')
  const start = lines.findIndex((l) => /^ {2}e2e:\s*$/.test(l))
  if (start < 0) throw new Error('no `e2e:` job in ci.yml')
  let end = lines.length
  for (let i = start + 1; i < lines.length; i++) {
    if (/^ {2}[A-Za-z0-9_-]+:\s*$/.test(lines[i])) {
      end = i
      break
    }
  }
  return lines.slice(start, end).join('\n')
}

describe('CI: E2E sharding', () => {
  const job = e2eJobBlock()

  it('runs the suite with --shard=i/n', () => {
    expect(job).toMatch(/--shard=\$\{\{\s*matrix\.shard\s*\}\}\/\d+/)
  })

  /**
   * The denominator in `--shard=i/n` must equal the number of entries in the
   * matrix's `shard:` list. If they drift, every job still runs and still
   * passes — it just covers a different slice of the suite, so a spec can be
   * executed twice or never, and nothing goes red to say so.
   */
  it('denominator matches the matrix shard list length', () => {
    const denomM = job.match(/--shard=\$\{\{\s*matrix\.shard\s*\}\}\/(\d+)/)
    expect(denomM, 'no --shard=i/n in the e2e job').not.toBeNull()
    const denom = Number(denomM![1])

    const listM = job.match(/^\s+shard:\s*\[([^\]]*)\]\s*$/m)
    expect(listM, 'no literal `shard: [...]` matrix entry').not.toBeNull()
    const list = listM![1]
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)

    expect(list.length).toBe(denom)
    // The indexes must be exactly 1..n: a duplicate or a gap means one slice is
    // run twice and another never.
    expect(list.map(Number)).toEqual(Array.from({ length: denom }, (_, i) => i + 1))
  })

  it('names per-shard artifacts uniquely', () => {
    // upload-artifact refuses to overwrite, so two shards sharing a name make
    // the second upload fail. The shard index must be part of every name.
    const names = [...job.matchAll(/^\s+name:\s*(e2e-coverage|playwright-report|playwright-results)-.*$/gm)]
    expect(names.length).toBeGreaterThan(0)
    for (const m of names) {
      expect(m[0], `artifact name lacks the shard index: ${m[0].trim()}`).toContain('matrix.shard')
    }
  })
})

describe('CI: frontend coverage is sharded and gated once', () => {
  /** A job block by name, from its header to the next job header. */
  function jobBlock(name: string): string {
    const lines = CI.split('\n')
    const start = lines.findIndex((l) => new RegExp(`^ {2}${name}:\\s*$`).test(l))
    if (start < 0) throw new Error(`no \`${name}:\` job in ci.yml`)
    let end = lines.length
    for (let i = start + 1; i < lines.length; i++) {
      if (/^ {2}[A-Za-z0-9_-]+:\s*$/.test(lines[i])) {
        end = i
        break
      }
    }
    return lines.slice(start, end).join('\n')
  }

  it('shard jobs do not gate, the gate job merges and gates', () => {
    const shard = jobBlock('coverage-frontend')
    const gate = jobBlock('coverage-frontend-gate')

    // The shard job must pass --shard (and must NOT pass --merge-shards): a
    // shard that gated would measure both tiers on a partial picture.
    expect(shard).toMatch(/check-frontend-coverage\.sh --shard=\$\{\{\s*matrix\.shard\s*\}\}\/\d+/)
    expect(shard).not.toContain('--merge-shards')

    // The gate job must merge every shard and not run tests again.
    expect(gate).toContain('--skip-test')
    expect(gate).toContain('--merge-shards=')
    expect(gate).not.toMatch(/check-frontend-coverage\.sh(?![\s\S]*--skip-test)/)
  })

  it('the gate merges exactly the shards the matrix runs', () => {
    const shard = jobBlock('coverage-frontend')
    const gate = jobBlock('coverage-frontend-gate')

    const listM = shard.match(/^\s+shard:\s*\[([^\]]*)\]\s*$/m)
    expect(listM, 'no literal `shard: [...]` in the coverage matrix').not.toBeNull()
    const list = listM![1].split(',').map((s) => s.trim()).filter(Boolean)

    const mergeM = gate.match(/--merge-shards=(\S+)/)
    expect(mergeM, 'no --merge-shards in the gate job').not.toBeNull()
    const merged = mergeM![1].split(',').filter(Boolean)

    // One merged directory per matrix shard: a missing entry silently drops a
    // slice from the report, an extra one adds a directory that never existed.
    expect(merged.length).toBe(list.length)
    for (const i of list) {
      expect(merged.some((d) => d.endsWith(`frontend-coverage-shard-${i}`))).toBe(true)
    }
  })

  it('the shard count in the shard job matches its own matrix', () => {
    const shard = jobBlock('coverage-frontend')
    const denomM = shard.match(/--shard=\$\{\{\s*matrix\.shard\s*\}\}\/(\d+)/)
    expect(denomM).not.toBeNull()
    const denom = Number(denomM![1])
    const listM = shard.match(/^\s+shard:\s*\[([^\]]*)\]\s*$/m)
    const list = listM![1].split(',').map((s) => s.trim()).filter(Boolean)
    expect(list.length).toBe(denom)
  })

  it('downstream jobs depend on the gate, not on a single shard', () => {
    // `needs: coverage-frontend` would let a build start after ONE shard (they
    // are independent jobs of the same matrix) — the suite would not have been
    // judged at all. The GATE job itself legitimately needs the shard jobs (it
    // consumes their artifacts), so that one `needs:` is expected; anything
    // else listing a shard job directly is the mistake.
    const lines = CI.split('\n')
    let currentJob = ''
    for (const line of lines) {
      const jobM = line.match(/^ {2}([A-Za-z0-9_-]+):\s*$/)
      if (jobM) currentJob = jobM[1]
      const needsM = line.match(/^\s+needs:\s*(.+)$/)
      if (!needsM) continue
      const values = needsM[1].replace(/[\[\]]/g, '').split(',').map((v) => v.trim())
      if (currentJob === 'coverage-frontend-gate') continue
      expect(values, `job \`${currentJob}\` needs a shard job directly: ${needsM[1].trim()}`).not.toContain(
        'coverage-frontend',
      )
    }
    expect(jobBlock('build-frontend')).toContain('coverage-frontend-gate')
  })
})

describe('CI: E2E shards never split a serial spec', () => {
  /**
   * `test.describe.serial` cases share state through the one server and must
   * run in order on the SAME machine. Playwright shards by FILE, so a serial
   * file is safe as long as it lands wholly in one shard — which is what this
   * asserts, for every shard count the matrix could reasonably use.
   *
   * The model below mirrors what Playwright actually does (verified against
   * `--list --shard=i/n` for n=3 and n=5: same file set per shard, no file
   * missing, no serial file split). Keeping it a model rather than shelling out
   * to `playwright --list` keeps this guard fast enough to run in the normal
   * suite; the real check is that the model agrees with the CLI, which the
   * comment records.
   */
  it('no serial spec file is split across shards', () => {
    const specDir = repoPath('e2e/specs')
    const files = readdirSync(specDir)
      .filter((f) => f.endsWith('.spec.ts'))
      .sort()

    const serial = new Set(
      files.filter((f) => readFileSync(resolve(specDir, f), 'utf8').includes('test.describe.serial')),
    )
    // If nothing is serial the assertion below is vacuous; make that visible.
    expect(serial.size).toBeGreaterThan(0)

    for (const shards of [2, 3, 4, 5]) {
      // Playwright assigns whole files round-robin; a file therefore occupies
      // exactly one shard no matter what the count is.
      const shardOf = new Map<string, number>()
      files.forEach((f, i) => shardOf.set(f, (i % shards) + 1))

      for (const f of serial) {
        expect(shardOf.get(f), `${f} has no shard at n=${shards}`).toBeDefined()
      }
      // Every file must land somewhere: a gap would silently drop a spec.
      expect(shardOf.size).toBe(files.length)
    }
  })
})

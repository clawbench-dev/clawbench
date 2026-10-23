import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Regression guard: npm publishing must be idempotent, and one tag must not
 * produce competing release pipelines.
 *
 * v0.121.0 shipped with a red `Publish to npm` job. The release itself was
 * fine — npm carried 0.121.0 — but the job died with:
 *
 *   npm error code E409
 *   npm error 409 Conflict - PUT https://registry.npmjs.org/@xulongzhe%2fclawbench
 *   npm error Cannot publish over previously staged version "0.121.0".
 *
 * Root cause: writing the tag ref several times (a delete-and-recreate, then a
 * recreation through the refs API because `git push` had not delivered a tag
 * push event) spawned four Release runs within five minutes. Two of them
 * reached the publish steps together. npm versions are immutable, so the loser
 * fails — but it fails *because someone else published it*, not because the
 * package is broken.
 *
 * Two invariants are needed together, and each is useless alone:
 *
 *   1. `concurrency` keyed on the ref collapses duplicate runs of one tag. It
 *      does not cover a re-run of the same tag started after the first one
 *      finished, nor two runs whose overlap the runner does not collapse.
 *   2. Every publish step must tolerate "already published". The `npm view`
 *      pre-check alone is NOT enough: publishing is staged, and on v0.121.0 the
 *      main package went from E409 to visible-in-`npm view` 71 seconds later.
 *      A step that checks once, publishes, and dies on the error still goes red.
 *
 * Scanned as text (comments stripped), matching the neighbouring
 * npmTrustedPublishing / releaseAssets guards. Structural checks are used so a
 * comment mentioning `npm publish` cannot satisfy them.
 */

function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

const RELEASE_YML = repoPath('.github/workflows/release.yml')
const PUBLISH_SCRIPT = repoPath('scripts/npm-publish-idempotent.sh')

/**
 * Drop YAML comment lines.
 *
 * Load-bearing: the concurrency block documents the E409 message verbatim, and
 * the publish steps carry comments explaining the race. Without stripping, the
 * assertions below would trip on their own documentation — and the tempting
 * "fix" would be to weaken the guard.
 */
function stripComments(src: string): string {
  return src
    .split('\n')
    .filter((l) => !/^\s*#/.test(l))
    .join('\n')
}

interface Job {
  key: string
  text: string
}

/** Split a workflow into jobs; job keys sit at two spaces under `jobs:`. */
function parseJobs(src: string): Job[] {
  const lines = stripComments(src).split('\n')
  const jobsAt = lines.findIndex((l) => /^jobs:\s*$/.test(l))
  if (jobsAt < 0) return []

  const jobs: Job[] = []
  let job: Job | null = null

  for (let i = jobsAt + 1; i < lines.length; i++) {
    const line = lines[i]
    const m = line.match(/^ {2}([A-Za-z0-9_-]+):\s*$/)
    if (m) {
      if (job) jobs.push(job)
      job = { key: m[1], text: line }
      continue
    }
    if (job) job.text += '\n' + line
  }
  if (job) jobs.push(job)
  return jobs
}

const SRC = stripComments(readFileSync(RELEASE_YML, 'utf8'))
const JOBS = parseJobs(readFileSync(RELEASE_YML, 'utf8'))
const PUBLISH_JOB_KEYS = ['publish-npm', 'publish-npm-desktop']

describe('release.yml — one tag runs one release pipeline', () => {
  it('declares a concurrency group keyed on the ref', () => {
    // Keyed on github.ref, not github.run_id: the goal is to collapse runs of
    // the same tag, while a new tag must never cancel the previous release.
    expect(
      SRC,
      'without `concurrency` a re-pushed tag starts a second full pipeline, ' +
        'and the two race on npm (immutable versions) and on the Release assets'
    ).toMatch(/^concurrency:\s*$/m)
    expect(SRC).toMatch(/^\s*group:\s*release-\$\{\{\s*github\.ref\s*\}\}\s*$/m)
    expect(
      SRC,
      'a new tag must not cancel an in-flight release of the previous tag'
    ).toMatch(/^\s*cancel-in-progress:\s*true\s*$/m)
  })
})

describe('release.yml — every publish step goes through the idempotent script', () => {
  it('parses the workflow into jobs', () => {
    // A parser matching nothing would make the per-job assertions vacuous.
    expect(JOBS.length).toBeGreaterThanOrEqual(5)
  })

  it.each(PUBLISH_JOB_KEYS)('%s calls the shared script for every publish', (key) => {
    const job = JOBS.find((j) => j.key === key)
    expect(job, `job ${key} not found`).toBeDefined()

    // Every `npm publish` must be delegated. A bare one has no race handling:
    // it dies with E409/E403 when a concurrent run wins, turning a shipped
    // release into a red job.
    const bare = job!.text
      .split('\n')
      .filter((l) => /npm publish/.test(l) && !/^\s*#/.test(l))
    expect(
      bare,
      `${key} still calls \`npm publish\` directly — route it through ` +
        `scripts/npm-publish-idempotent.sh so a concurrent publish is not a failure`
    ).toEqual([])

    expect(
      job!.text,
      `${key} must publish via scripts/npm-publish-idempotent.sh`
    ).toMatch(/npm-publish-idempotent\.sh/)
  })
})

describe('npm-publish-idempotent.sh — tolerates an already-published version', () => {
  const script = readFileSync(PUBLISH_SCRIPT, 'utf8')

  it('skips a version that is already on the registry', () => {
    expect(
      script,
      'a re-run of a release must not attempt to republish an immutable version'
    ).toMatch(/npm view "\$NAME@\$VERSION" version/)
  })

  // The polling behaviour itself is covered by
  // npmPublishIdempotent.behaviour.test.ts, which runs the script against a
  // fake npm. A source-text check cannot distinguish "polls" from "polls zero
  // times" — setting the wait budget to 0 left an earlier version of this
  // assertion green, which is exactly the shape that shipped and failed.

  it('derives name and version from the package manifest, not from arguments', () => {
    // Deriving them from the directory keeps the skip check and the publish
    // pointed at the same package; a caller passing the wrong version string
    // would otherwise silently check a different version than it publishes.
    expect(script).toMatch(/package\.json/)
    expect(script).toMatch(/NAME=\$\(node -p/)
    expect(script).toMatch(/VERSION=\$\(node -p/)
  })
})

import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Regression guard: CI must build the isolated Excalidraw vendor bundle.
 *
 * The `.excalidraw` editor is an independent React build that lives in
 * `web/vendor-build/excalidraw/` with its own package.json. It is deliberately
 * NOT part of the root Vite build (React + @excalidraw/excalidraw is ~8MB and
 * must never bloat the main Vue bundle), so the root `npm run build` does not
 * emit it — only `build.sh` step 1a does.
 *
 * That asymmetry caused a real outage: every CI job and every release job ran
 * the root build and copied `.clawbench-web/` into `internal/frontend/dist` for
 * go:embed, but nothing ever built the vendor bundle. The released binaries
 * therefore embedded a frontend with no `vendor/excalidraw/` at all, and
 * opening a `.excalidraw` file rendered the server's plain
 * `404 page not found` inside the iframe (ServeIndex falls through to
 * http.NotFound when the path is absent from the FS). Local builds worked
 * because `build.sh` did build it — so the bug was invisible until release.
 *
 * The guard is structural rather than a count: it walks the workflow jobs and
 * requires that any job producing an embeddable frontend also builds the vendor
 * bundle. That way a new job copied from an existing one cannot silently
 * reintroduce the gap.
 *
 * Scanned as text rather than parsed as YAML: js-yaml is only a transitive
 * dependency here, and the neighbouring releaseAssets/versionCode guards read
 * these same files the same way.
 */

/** Locate a repo file, tolerating either cwd the suite may run from. */
function repoPath(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    const p = resolve(base, rel)
    if (existsSync(p)) return p
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

/** The marker every vendor-build step carries. */
const VENDOR_DIR = 'web/vendor-build/excalidraw'

interface Step {
  name: string
  /** All lines of the step, joined — used for run-body and key matching. */
  text: string
  hasVendorDir: boolean
  /** The step's `run:` body, or '' for `uses:` steps. */
  run: string
}

interface Job {
  name: string
  steps: Step[]
}

/**
 * Split a workflow into jobs → steps.
 *
 * Jobs are the keys indented two spaces directly under `jobs:`; a step starts
 * at `      - ` (six spaces + dash) and owns every following line until the next
 * step or job. Scanning stops at the next job so a step's lines are never
 * attributed to the wrong job.
 */
function parseJobs(src: string): Job[] {
  const lines = src.split('\n')

  // Everything before `jobs:` is workflow-level (name/on/env) — skip it, so a
  // top-level `env:` key cannot be mistaken for a job.
  const jobsAt = lines.findIndex((l) => /^jobs:\s*$/.test(l))
  if (jobsAt < 0) return []

  const jobs: Job[] = []
  let job: Job | null = null
  let step: Step | null = null

  const flushStep = () => {
    if (step && job) {
      step.text = step.text.replace(/\s+$/, '')
      // A `run:` block is the only place a build command can hide. For a
      // `uses:` step (or a step with no run), leave it empty.
      const runIdx = step.text.indexOf('\n        run:')
      step.run = runIdx >= 0 ? step.text.slice(runIdx) : ''
      step.hasVendorDir = step.text.includes(VENDOR_DIR)
      job.steps.push(step)
    }
    step = null
  }
  const flushJob = () => {
    flushStep()
    if (job) jobs.push(job)
    job = null
  }

  for (let i = jobsAt + 1; i < lines.length; i++) {
    const line = lines[i]

    // A job header: exactly two spaces of indent, a bare `key:`.
    const jobM = line.match(/^ {2}([A-Za-z0-9_-]+):\s*$/)
    if (jobM) {
      flushJob()
      job = { name: jobM[1], steps: [] }
      continue
    }
    if (!job) continue

    // A step header: six spaces, a dash, then a key.
    if (/^ {6}- /.test(line)) {
      flushStep()
      const nameM = line.match(/^ {6}- name:\s*(.+?)\s*$/)
      step = { name: nameM ? nameM[1] : '', text: line, hasVendorDir: false, run: '' }
      continue
    }
    if (step) step.text += '\n' + line
  }
  flushJob()

  return jobs
}

/**
 * True when a step runs the ROOT frontend build — the one whose output lands in
 * `.clawbench-web/` and gets embedded.
 *
 * Three shapes must be excluded, or the check would match steps that have
 * nothing to do with the embedded frontend:
 *   - the vendor step itself (it also runs `npm run build`, inside its own dir)
 *   - desktop packaging (`cd desktop` then build; produces no .clawbench-web)
 *   - unrelated `npm run <other>` scripts (no `npm run build` at all)
 */
function isRootFrontendBuild(step: Step): boolean {
  if (step.hasVendorDir) return false
  if (!/npm run build(\s|$|")/.test(step.run)) return false
  if (/cd desktop/.test(step.run)) return false
  return true
}

function isVendorBuild(step: Step): boolean {
  return step.hasVendorDir && /npm run build(\s|$|")/.test(step.run)
}

const CI_YML = repoPath('.github/workflows/ci.yml')
const RELEASE_YML = repoPath('.github/workflows/release.yml')

function readWorkflow(p: string): string {
  return readFileSync(p, 'utf8')
}

describe.each([
  ['ci.yml', CI_YML],
  ['release.yml', RELEASE_YML],
])('%s — the embedded frontend includes the Excalidraw vendor bundle', (_label, path) => {
  const jobs = parseJobs(readWorkflow(path))

  it('parses the workflow into jobs', () => {
    // A parser that matched nothing would make every assertion below pass
    // vacuously. Fail loudly instead.
    expect(jobs.length).toBeGreaterThanOrEqual(5)
  })

  it('builds the vendor bundle in every job that runs the root frontend build', () => {
    const offenders: string[] = []
    for (const job of jobs) {
      const rootBuilds = job.steps.filter(isRootFrontendBuild)
      if (rootBuilds.length === 0) continue
      if (!job.steps.some(isVendorBuild)) {
        offenders.push(`${job.name} (step: ${rootBuilds.map((s) => s.name).join(', ')})`)
      }
    }
    expect(
      offenders,
      `job(s) embed a frontend without building ${VENDOR_DIR} — the iframe at ` +
        `/vendor/excalidraw/index.html will 404: ${offenders.join('; ')}`
    ).toEqual([])
  })

  it('runs the vendor build as a real build, not just an install', () => {
    // `npm ci` alone produces no output, so a step that only installs would
    // satisfy a naive "mentions the directory" check while still shipping a
    // broken editor.
    const vendorSteps = jobs.flatMap((j) => j.steps.filter(isVendorBuild))
    expect(vendorSteps.length).toBeGreaterThan(0)
  })
})

describe('release.yml — the web-dist artifact carries the vendor bundle', () => {
  const jobs = parseJobs(readWorkflow(RELEASE_YML))

  it('builds the vendor bundle in the job that uploads web-dist', () => {
    // This is the load-bearing one. The `web-dist` artifact is downloaded by
    // every platform job and copied into internal/frontend/dist for go:embed,
    // so anything missing from it is missing from every released binary — not
    // just one platform. The job must build the vendor bundle *before* the
    // upload, otherwise the artifact is captured without it.
    const producer = jobs.find((j) =>
      j.steps.some((s) => s.text.includes('upload-artifact') && s.text.includes('web-dist'))
    )
    expect(producer, 'no job uploads the web-dist artifact').toBeDefined()

    const vendorIdx = producer!.steps.findIndex(isVendorBuild)
    const uploadIdx = producer!.steps.findIndex(
      (s) => s.text.includes('upload-artifact') && s.text.includes('web-dist')
    )
    expect(vendorIdx, `web-dist job does not build ${VENDOR_DIR}`).toBeGreaterThanOrEqual(0)
    expect(
      vendorIdx,
      'the vendor bundle is built after the web-dist upload, so it is not captured'
    ).toBeLessThan(uploadIdx)
  })
})

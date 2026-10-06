import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Regression guard: npm publishing must use OIDC trusted publishing, not a
 * long-lived NPM_TOKEN.
 *
 * The failure this prevents is nasty because npm reports it as a 404. A GitHub
 * Secret of type "npm granular access token" silently expires (90 days by
 * default), and once it does every publish step dies with:
 *
 *   npm error code E404
 *   npm error 404 Not Found - PUT https://registry.npmjs.org/@scope%2fname
 *   npm error 404 The requested resource '...' could not be found
 *
 * That reads like "the package does not exist" or "you lost permission", not
 * "your token expired" — so it was diagnosed by hand three releases in a row
 * (v0.110/111/112 never reached npm) before anyone noticed the gap.
 *
 * Trusted publishing removes the expiring credential entirely, but it only
 * works when three things hold together. Each has its own failure mode, and
 * two of them look like the 404 above:
 *
 *   1. `id-token: write` on the job, or GitHub mints no OIDC token at all.
 *   2. No token configured. npm tries a configured token first and only falls
 *      back to OIDC when there is none — and `_authToken=${NODE_AUTH_TOKEN}`
 *      in an .npmrc counts as configured even when the variable is unset,
 *      because npm sends the literal placeholder string as the token. That is
 *      why setup-node's `registry-url` is also forbidden here: it writes
 *      exactly that line.
 *   3. `repository.url` in each package.json matching the GitHub repo, or
 *      provenance verification rejects the publish.
 *
 * Scanned as text (comments stripped) rather than parsed as YAML: js-yaml is
 * only a transitive dependency, and the neighbouring excalidrawVendorBuild /
 * releaseAssets / versionCode guards read these same files the same way.
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

/** The repository the trusted publishers were configured against. */
const REPO_SLUG = 'clawbench-dev/clawbench'

/**
 * Drop YAML comment lines.
 *
 * Load-bearing, not cosmetic: the workflow explains this trap in comments that
 * spell out `_authToken=${NODE_AUTH_TOKEN}` verbatim. Without stripping, the
 * guard would fail on its own documentation — and the tempting "fix" would be
 * to weaken the assertion, which is how a guard quietly stops guarding.
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

/**
 * Split a workflow into its jobs.
 *
 * Job keys sit at exactly two spaces of indent directly under `jobs:`; a job
 * owns every line until the next such key, so a token in one job can never be
 * attributed to another. Everything before `jobs:` is skipped so a top-level
 * `env:` cannot be mistaken for a job.
 */
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

const JOBS = parseJobs(readFileSync(RELEASE_YML, 'utf8'))

/** The two jobs that run `npm publish`. */
const PUBLISH_JOB_KEYS = ['publish-npm', 'publish-npm-desktop']

describe('release.yml — npm publishing uses OIDC trusted publishing', () => {
  it('parses the workflow into jobs', () => {
    // A parser that matched nothing would make every assertion below pass
    // vacuously. Fail loudly instead.
    expect(JOBS.length).toBeGreaterThanOrEqual(5)
  })

  it.each(PUBLISH_JOB_KEYS)('%s exists', (key) => {
    expect(JOBS.find((j) => j.key === key)).toBeDefined()
  })

  it.each(PUBLISH_JOB_KEYS)('%s grants id-token: write', (key) => {
    const job = JOBS.find((j) => j.key === key)
    expect(job, `job ${key} not found`).toBeDefined()
    expect(
      job!.text,
      `${key} cannot mint an OIDC token without \`id-token: write\`, so ` +
        `trusted publishing is dead and publish fails with a 404`
    ).toMatch(/^\s*id-token:\s*write\s*$/m)
  })

  it.each(PUBLISH_JOB_KEYS)('%s configures no npm token', (key) => {
    const job = JOBS.find((j) => j.key === key)!
    // Any of these makes npm prefer token auth over OIDC. `_authToken` is the
    // .npmrc form, `NODE_AUTH_TOKEN` the env form setup-node wires up, and
    // `secrets.NPM_TOKEN` the long-lived credential that expired unnoticed.
    for (const pattern of [/NODE_AUTH_TOKEN/, /secrets\.NPM_TOKEN/, /_authToken/]) {
      expect(
        job.text,
        `${key} must not configure an npm token (${pattern}): npm tries a ` +
          `configured token before OIDC, so trusted publishing would be skipped`
      ).not.toMatch(pattern)
    }
  })

  it.each(PUBLISH_JOB_KEYS)('%s does not set setup-node registry-url', (key) => {
    const job = JOBS.find((j) => j.key === key)!
    // registry-url makes setup-node write `_authToken=${NODE_AUTH_TOKEN}` into
    // an .npmrc, which counts as a configured token even with the var unset.
    expect(
      job.text,
      `${key} must not set setup-node's registry-url — it writes the ` +
        `\`_authToken=\${NODE_AUTH_TOKEN}\` placeholder that disables OIDC`
    ).not.toMatch(/^\s*registry-url:/m)
  })

  it.each(PUBLISH_JOB_KEYS)('%s still points npm at the public registry', (key) => {
    // Dropping registry-url must not also drop the registry override: the
    // project .npmrc sets registry=npmmirror, which project config makes win
    // over everything else, and the mirror cannot accept a publish.
    const job = JOBS.find((j) => j.key === key)!
    expect(job.text).toMatch(/registry=https:\/\/registry\.npmjs\.org\//)
  })

  it.each(PUBLISH_JOB_KEYS)('%s writes the registry into the project .npmrc', (key) => {
    // This is the exact bug the first version of this workflow shipped: the
    // redirect was `>> "${NPM_CONFIG_USERCONFIG}"`, but that variable is only
    // exported by setup-node when `registry-url` is set — and registry-url is
    // (correctly) gone for OIDC. The redirect therefore expanded to `>> ""`
    // and the step died with "No such file or directory" before publishing
    // anything, in both publish jobs, on the v0.112.1 release.
    //
    // A project .npmrc is read from the working directory npm runs in and
    // needs no env var, so it is the only safe target here.
    const job = JOBS.find((j) => j.key === key)!
    expect(
      job.text,
      `${key} must write the registry into the project .npmrc (a bare ` +
        `\`> .npmrc\`), not append to $NPM_CONFIG_USERCONFIG — that variable ` +
        `is unset once registry-url is removed`
    ).toMatch(/>\s*\.npmrc\b/)
    expect(
      job.text,
      `${key} must not reference $NPM_CONFIG_USERCONFIG`
    ).not.toMatch(/NPM_CONFIG_USERCONFIG/)
  })
})

describe('npm package manifests declare the publishing repository', () => {
  /**
   * Every publishable package.json.
   *
   * Two shapes coexist under npm/: `main/` and the payload/`platforms` groups
   * (`<group>/<name>/`). Check each entry for a manifest before descending, so
   * a leaf package is not skipped just because it has no sub-packages.
   */
  function findManifests(): string[] {
    const out: string[] = []
    const npmDir = repoPath('npm')
    const consider = (dir: string) => {
      const p = resolve(dir, 'package.json')
      if (existsSync(p)) out.push(p)
    }
    for (const group of readdirSync(npmDir, { withFileTypes: true })) {
      if (!group.isDirectory()) continue
      const groupDir = resolve(npmDir, group.name)
      consider(groupDir)
      for (const entry of readdirSync(groupDir, { withFileTypes: true })) {
        if (entry.isDirectory()) consider(resolve(groupDir, entry.name))
      }
    }
    return out.sort()
  }

  const manifests = findManifests()

  it('finds every published package', () => {
    // 7 server packages + 3 desktop payloads. A count that silently dropped
    // would make the per-file assertions below vacuous.
    expect(manifests.length).toBe(10)
  })

  it.each(manifests)('%s has repository.url matching the trusted publisher', (path) => {
    const pkg = JSON.parse(readFileSync(path, 'utf8'))
    // npm verifies this against the OIDC token's repository claim; a mismatch
    // or absence rejects the publish.
    expect(
      pkg.repository?.url,
      `${pkg.name} is missing repository.url — provenance verification ` +
        `requires it to match ${REPO_SLUG}`
    ).toBe(`git+https://github.com/${REPO_SLUG}.git`)
  })
})

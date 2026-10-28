/**
 * Reads the generated GitGraph fixture repos.
 *
 * The repos under `test/git-graph-repos/` are built by
 * `scripts/gen-git-graph-fixtures.mjs` from `fixtures.json` and are
 * gitignored — they are real git repositories, not data files. This module is
 * the only thing that reads them, so the tests assert against what `git`
 * actually reports rather than against a hand-copied snapshot that can drift.
 *
 * If the repos are missing (fresh clone), they are built on demand: the test
 * file calls `ensureFixtures()` once in `beforeAll`.
 */

import { execFileSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
export const FIXTURE_ROOT = resolve(here, '../../../../test/git-graph-repos')
const GENERATOR = resolve(here, '../../../../scripts/gen-git-graph-fixtures.mjs')

/** Builds any fixture repos that are missing. Safe to call repeatedly. */
export function ensureFixtures() {
  if (existsSync(join(FIXTURE_ROOT, 'fixtures.json'))) {
    const missing = ['01-linear', '02-single-merge', '03-multi-branch', '04-frequent-merge',
      '05-long-lived-branch', '06-octopus-merge', '07-open-branches', '08-long-linear']
      .some((d) => !existsSync(join(FIXTURE_ROOT, d)))
    if (!missing) return
  }
  execFileSync(process.execPath, [GENERATOR], { stdio: 'pipe' })
}

function git(repo, args) {
  return execFileSync('git', args, {
    cwd: join(FIXTURE_ROOT, repo),
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  }).trim()
}

/**
 * Returns commits in the shape the graph code consumes, newest first:
 * `{ sha, parents, msg, refs }`.
 *
 * `limit` truncates to the newest N commits — the partial-loading tests rely
 * on a parent pointing outside the returned list.
 */
export function readRepo(repo, { limit } = {}) {
  const branchHeads = new Map()
  for (const line of git(repo, ['for-each-ref', '--format=%(refname:short) %(objectname)', 'refs/heads/']).split('\n')) {
    if (!line) continue
    const [name, sha] = line.split(' ')
    branchHeads.set(sha, name)
  }

  // --all so commits reachable only from a side branch are included;
  // --topo-order keeps children before parents, which the graph expects.
  const rows = git(repo, ['log', '--all', '--topo-order', '--format=%H|%P|%s'])
    .split('\n')
    .filter(Boolean)

  const headSha = git(repo, ['rev-parse', 'HEAD'])
  const commits = rows.map((row) => {
    const [sha, parents, msg] = row.split('|')
    const refs = []
    if (sha === headSha) refs.push('HEAD')
    const branch = branchHeads.get(sha)
    if (branch) refs.push(branch)
    return {
      sha: sha.slice(0, 7),
      parents: parents ? parents.split(' ').map((p) => p.slice(0, 7)) : [],
      msg,
      ...(refs.length ? { refs } : {}),
    }
  })

  return limit ? commits.slice(0, limit) : commits
}

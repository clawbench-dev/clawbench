#!/usr/bin/env node
/**
 * Builds the GitGraph fixture repos under `test/git-graph-repos/` from
 * `test/git-graph-repos/fixtures.json`.
 *
 * Why generated instead of committed: the fixtures are 8 nested git repos.
 * Committing them records 8 gitlinks with no `.gitmodules` entry, which makes
 * `git submodule update --init --recursive` fail outright — that is how
 * Cloudflare Pages broke. Keeping only the topology in JSON avoids the
 * gitlinks entirely while keeping the shapes reviewable in a diff.
 *
 * Commits are created with `git commit-tree` rather than checkout/merge, so
 * the result depends only on (tree, parents, message, identity, timestamp) —
 * no working-tree state, no merge conflicts, no ordering surprises. The
 * identity and clock are pinned, so a given fixtures.json always produces the
 * same SHAs on every machine.
 *
 * Usage:
 *   node scripts/gen-git-graph-fixtures.mjs           # build any that are missing
 *   node scripts/gen-git-graph-fixtures.mjs --force   # rebuild all
 */

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, rmSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const OUT = join(ROOT, 'test/git-graph-repos')
const SPEC = join(OUT, 'fixtures.json')

/** Fixed identity and clock — changing either changes every SHA. */
const IDENT = {
  GIT_AUTHOR_NAME: 'Test',
  GIT_AUTHOR_EMAIL: 'test@test.com',
  GIT_COMMITTER_NAME: 'Test',
  GIT_COMMITTER_EMAIL: 'test@test.com',
  GIT_AUTHOR_DATE: '2026-04-26 12:01:13 +0800',
  GIT_COMMITTER_DATE: '2026-04-26 12:01:13 +0800',
}

function git(cwd, args, input) {
  return execFileSync('git', args, {
    cwd,
    env: { ...process.env, ...IDENT },
    encoding: 'utf8',
    input,
    stdio: [input === undefined ? 'ignore' : 'pipe', 'pipe', 'pipe'],
  }).trim()
}

function build(dir, fixture) {
  mkdirSync(dir, { recursive: true })
  git(dir, ['init', '-q', '-b', fixture.head])

  // fixtures.json lists commits newest-first; git needs them oldest-first.
  const commits = [...fixture.commits].reverse()
  const byLogical = new Map() // logical sha -> real sha

  for (const c of commits) {
    // A unique blob per commit, so no two commits share a tree. The content is
    // the logical id — stable, and it makes `git show` self-describing.
    const blob = git(dir, ['hash-object', '-w', '--stdin'], `${c.sha}\n`)
    const tree = git(
      dir,
      ['mktree'],
      `100644 blob ${blob}\t${c.sha}.txt\n`,
    )

    const parents = c.parents.map((p) => {
      const real = byLogical.get(p)
      // A parent outside the listed commits is a deliberately truncated
      // history (08-long-linear records only the first 10 of 40). Drop it —
      // the commit becomes a root here, which is what the test's partial-load
      // assertions expect.
      return real ? ['-p', real] : []
    }).flat()

    const sha = git(dir, ['commit-tree', tree, ...parents, '-m', c.msg])
    byLogical.set(c.sha, sha)
  }

  // Point branches at the commits whose `refs` list them. `HEAD -> master`
  // means the branch named after the arrow; a bare `HEAD` is not a branch.
  const branchSet = new Set()
  for (const c of fixture.commits) {
    for (const ref of c.refs ?? []) {
      const branch = ref.replace(/^HEAD -> /, '')
      if (branch === 'HEAD') continue
      git(dir, ['update-ref', `refs/heads/${branch}`, byLogical.get(c.sha)])
      branchSet.add(branch)
    }
  }

  // Not every fixture annotates its head commit, so create the branch if the
  // refs above never mentioned it. `commits` was reversed to build order, so
  // the newest commit is now last.
  if (!branchSet.has(fixture.head)) {
    git(dir, ['update-ref', `refs/heads/${fixture.head}`, byLogical.get(commits.at(-1).sha)])
  }

  // Leave HEAD on the declared branch rather than detached.
  git(dir, ['symbolic-ref', 'HEAD', `refs/heads/${fixture.head}`])
  git(dir, ['reset', '-q', '--hard', 'HEAD'])
}

const force = process.argv.includes('--force')
const spec = JSON.parse(readFileSync(SPEC, 'utf8'))
let made = 0
let kept = 0

for (const [name, fixture] of Object.entries(spec.fixtures)) {
  const dir = join(OUT, name)
  if (existsSync(dir)) {
    if (!force) {
      kept++
      continue
    }
    rmSync(dir, { recursive: true, force: true })
  }
  build(dir, fixture)
  made++
  const head = git(dir, ['rev-parse', '--short', 'HEAD'])
  const count = git(dir, ['rev-list', '--all', '--count'])
  console.log(`  ${name.padEnd(22)} ${count.padStart(3)} commits  HEAD=${head}`)
}

console.log(
  made || kept
    ? `\n${made} generated, ${kept} already present (use --force to rebuild).`
    : '\nNothing to do.',
)

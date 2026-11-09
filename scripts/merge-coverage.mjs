#!/usr/bin/env node
/**
 * merge-coverage.mjs — merge per-shard Istanbul coverage into one report.
 *
 * The frontend suite is run in shards on CI (see the `coverage-frontend` job),
 * because a single 4-core runner needs ~16 min for it and that made it the
 * pipeline's critical path once the E2E suite was parallelised. Each shard
 * writes its own `coverage/coverage-final.json`; this script folds them into a
 * single `coverage-final.json` + `coverage-summary.json` in the SAME shape
 * vitest produces, so `check-frontend-coverage.sh` reads it unchanged.
 *
 * The merge is `istanbul-lib-coverage`'s, which is what the coverage tooling
 * itself uses: statement/branch/function maps are keyed by position, so two
 * shards covering different lines of one file combine correctly, and a line
 * covered by either shard counts as covered.
 *
 * Usage: node scripts/merge-coverage.mjs <out-dir> <shard-dir>...
 *   Each shard dir must contain coverage-final.json (vitest's json reporter).
 */

import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const { createCoverageMap } = require('istanbul-lib-coverage')

const [outDir, ...shardDirs] = process.argv.slice(2)
if (!outDir || shardDirs.length === 0) {
  console.error('usage: merge-coverage.mjs <out-dir> <shard-dir>...')
  process.exit(2)
}

const merged = createCoverageMap({})
let used = 0
const missing = []

for (const dir of shardDirs) {
  const file = join(dir, 'coverage-final.json')
  if (!existsSync(file)) {
    // A shard that crashed before writing its report leaves a gap in the
    // picture. Report it by name: a silent skip would make the merged number
    // look like a coverage DROP, and the gate would blame the code.
    missing.push(dir)
    continue
  }
  try {
    merged.merge(JSON.parse(readFileSync(file, 'utf8')))
    used++
  } catch (err) {
    console.error(`merge-coverage: ${file} is unreadable: ${err.message}`)
    process.exit(1)
  }
}

if (used === 0) {
  console.error('merge-coverage: no shard produced coverage-final.json')
  process.exit(1)
}
if (missing.length > 0) {
  console.error(
    `merge-coverage: ${missing.length} shard(s) produced no coverage and are NOT in this report: ${missing.join(', ')}`,
  )
  console.error('merge-coverage: the totals below are for the remaining shards only')
}

mkdirSync(outDir, { recursive: true })
writeFileSync(join(outDir, 'coverage-final.json'), JSON.stringify(merged.toJSON()))

// coverage-summary.json in vitest's shape: { "<abs path>": {lines:{...}, ...}, total: {...} }
const summary = { total: merged.getCoverageSummary().toJSON() }
for (const path of merged.files()) {
  summary[path] = merged.fileCoverageFor(path).toSummary().toJSON()
}
writeFileSync(join(outDir, 'coverage-summary.json'), JSON.stringify(summary))

const s = summary.total
console.log(
  `merge-coverage: merged ${used} shard(s) → ${merged.files().length} file(s), ` +
    `lines ${s.lines.covered}/${s.lines.total} (${s.lines.pct}%)`,
)

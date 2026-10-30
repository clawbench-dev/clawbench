#!/usr/bin/env node
// Regenerate internal/model/provider_presets.json from a models.dev snapshot.
//
// The settings page offers these as "fill the form for me" presets. They carry
// no secrets and are never authoritative — a saved provider row always wins.
//
// Usage:
//   node scripts/gen-provider-presets.mjs [path-to-models.json]
//
// Default source: ~/.cache/opencode/models.json (OpenCode caches models.dev
// there). Any file with the same shape works.
import { readFileSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const source = process.argv[2] || join(homedir(), '.cache/opencode/models.json')

/** Map the SDK package to the wire protocol we can inject. */
function protocolOf(npm) {
  if (npm === '@ai-sdk/anthropic') return 'anthropic'
  if (npm === '@ai-sdk/openai' || npm === '@ai-sdk/openai-compatible') return 'openai'
  if (npm === '@ai-sdk/google' || npm === '@ai-sdk/google-vertex') return 'gemini'
  return null
}

const catalog = JSON.parse(readFileSync(source, 'utf8'))
const out = []
for (const [id, entry] of Object.entries(catalog)) {
  const protocol = protocolOf(entry.npm)
  let baseUrl = (entry.api || '').trim()
  if (!protocol || !baseUrl.startsWith('http')) continue
  // Anthropic CLIs append /v1/messages themselves, so the injected base URL
  // must not already end in /v1 (models.dev's SDK base does).
  if (protocol === 'anthropic' && baseUrl.endsWith('/v1')) baseUrl = baseUrl.slice(0, -3)
  out.push({ id, name: (entry.name || id).trim(), protocol, baseUrl })
}

out.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.id.localeCompare(b.id))
const target = join(root, 'internal/model/provider_presets.json')
writeFileSync(target, JSON.stringify(out, null, 1) + '\n')
console.log(`wrote ${out.length} presets to ${target}`)

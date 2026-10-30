#!/usr/bin/env node
/**
 * Builds the remote-control helper that ships inside the desktop app.
 *
 * It is a tiny Go program (pure syscall, no CGO) so the app needs no native npm
 * module and the user installs nothing extra. Cross-compiled for Windows from
 * any host, because that is the only platform this app shares a screen from.
 *
 * The output lives in desktop/assets/ so electron-builder can pick it up as an
 * extraResource; it is generated, not committed.
 */
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const repoRoot = join(here, '..', '..')
const outDir = join(here, '..', 'assets')
const out = join(outDir, 'input-helper.exe')

if (!existsSync(outDir)) mkdirSync(outDir, { recursive: true })

try {
  execFileSync('go', ['build', '-trimpath', '-ldflags', '-s -w', '-o', out, './cmd/input-helper'], {
    cwd: repoRoot,
    stdio: 'inherit',
    env: { ...process.env, GOOS: 'windows', GOARCH: 'amd64', CGO_ENABLED: '0' },
  })
  console.log(`input-helper: built ${out}`)
} catch (e) {
  console.error('input-helper: build failed. The desktop package needs a Go toolchain')
  console.error('(the helper is what lets a viewer drive the host pointer/keyboard).')
  throw e
}

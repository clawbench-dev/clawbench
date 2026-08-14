import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'
import { realpathSync } from 'fs'

// In git worktrees, web/node_modules may be a symlink into the primary
// checkout's node_modules. Vite's fs.allow by default denies serving files
// whose realpath lies outside the project root, which breaks imports such as
// @lobehub/icons-static-svg SVG assets. Resolve the symlink's real target and
// add it to the allowlist so tests run identically in worktrees.
const webNodeModules = resolve(__dirname, 'web/node_modules')
let webNodeModulesReal: string | null = null
try {
  webNodeModulesReal = realpathSync(webNodeModules)
} catch {
  // web/node_modules missing or not a symlink — nothing extra to allow
}

// Vite plugin: resolve static asset references (e.g., /logo.png) to the
// actual file in the assets directory so they can be found during tests.
// In production Vite serves these from publicDir, but during unit tests
// the module resolver needs an explicit alias.
function staticAssetResolver(): import('vite').Plugin {
  return {
    name: 'static-asset-resolver',
    resolveId(source) {
      if (source === '/logo.png') {
        return resolve(__dirname, 'assets/logo.png')
      }
    },
  }
}

export default defineConfig({
  plugins: [vue(), staticAssetResolver()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'web/src'),
      // The frontend lives in web/ with its own node_modules, but the root
      // also installs Vue. Without forcing a single copy, @vue/test-utils
      // (resolved from root) and SFC components (resolved from web/node_modules)
      // load two separate Vue reactivity instances — setup refs change but the
      // test-utils renderer never re-renders. Pin all Vue modules to the root
      // copy so reactivity connects (see pre-existing ShortcutTipTicker failure).
      vue: resolve(__dirname, 'node_modules/vue'),
      '@vue/reactivity': resolve(__dirname, 'node_modules/@vue/reactivity'),
      '@vue/runtime-core': resolve(__dirname, 'node_modules/@vue/runtime-core'),
      '@vue/runtime-dom': resolve(__dirname, 'node_modules/@vue/runtime-dom'),
      '@vue/shared': resolve(__dirname, 'node_modules/@vue/shared'),
      '@vue/server-renderer': resolve(__dirname, 'node_modules/@vue/server-renderer'),
    },
  },
  publicDir: resolve(__dirname, 'assets'),
  server: {
    fs: {
      allow: [__dirname, ...(webNodeModulesReal ? [webNodeModulesReal] : [])],
    },
  },
  test: {
    environment: 'jsdom',
    css: true,
    // Detect async resource leaks (unclosed timers, sockets) in test files.
    // Helps identify which tests cause worker processes to hang on exit.
    detectAsyncLeaks: true,
    // Teardown timeout: vitest waits this long for workers to close after
    // tests finish. If workers have open handles (Vite server FILEHANDLEs,
    // vue-i18n enableDevTools promise), they can't exit cleanly.
    // Set to 5s (reduced from default 10s) so the globalSetup worker kill
    // timer fires sooner, unblocking pool.close() to write coverage data.
    teardownTimeout: 5_000,
    // Force-exit safety net for vitest 4.x pool cleanup hang bug.
    // See vitest-dev/vitest#8766, #9494, #8861, #9123.
    globalSetup: [resolve(__dirname, 'vitest-globalSetup.ts')],
    // 'hanging-process' reporter warns when tests leave open handles
    // (timers, sockets, etc.) that prevent worker processes from exiting.
    reporters: ['default', 'hanging-process'],
    // Use 'forks' pool. Vitest 4.x has a known bug where fork workers can
    // become zombies on pool cleanup (vitest-dev/vitest#8766). Mitigated by:
    // - vitest-globalSetup.ts: worker kill timer to unblock pool.close()
    // - scripts/vitest-run.sh: watchdog timeout + process tree kill
    // We use 'forks' (not 'threads') because with threads, open handles
    // (setInterval, addEventListener) in test components keep the shared
    // main process event loop alive, preventing globalSetup teardown() from
    // ever running. With forks, teardown() runs in the main process even if
    // worker child processes hang.
    pool: 'forks',
    // Vitest 4 migration: poolOptions removed, all options promoted to top-level.
    // Don't set maxWorkers — let vitest auto-detect (numCPUs-1).
    // The pool cleanup hang is mitigated by vitest-globalSetup.ts worker kill.
    exclude: [
      '**/.worktrees/**',
      '**/.codebuddy/worktrees/**',
      '**/node_modules/**',
      '**/dist/**',
      '**/cypress/**',
      '**/e2e/**',
      '**/.{idea,git,cache,output,temp}/**',
      '**/test/path-annotation/**',
    ],
    coverage: {
      reporter: ['text', 'json', 'json-summary'],
      // Generate coverage reports even when tests fail. Without this,
      // onTestFailure() calls cleanAfterRun() which deletes .tmp coverage
      // files before generateCoverage() can read them.
      reportOnFailure: true,
    },
    setupFiles: [resolve(__dirname, 'web/src/test-setup.ts')],
  },
})

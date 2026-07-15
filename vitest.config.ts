import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

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

// Vite plugin: patch Vue 3.5 renderSlot null-safety for test environment.
// Vue 3.5.x accesses currentRenderingInstance.ce without a null check in
// renderSlot(), which causes TypeError when mocked components render slots
// outside of a component context (currentRenderingInstance is null).
function vueRenderSlotNullGuard(): import('vite').Plugin {
  return {
    name: 'vue-renderslot-null-guard',
    enforce: 'pre',
    transform(code, id) {
      if (!id.includes('@vue/runtime-core')) return
      const pattern = 'if (currentRenderingInstance.ce || currentRenderingInstance.parent && isAsyncWrapper(currentRenderingInstance.parent) && currentRenderingInstance.parent.ce) {'
      const replacement = 'if (currentRenderingInstance && (currentRenderingInstance.ce || currentRenderingInstance.parent && isAsyncWrapper(currentRenderingInstance.parent) && currentRenderingInstance.parent.ce)) {'
      if (code.includes(pattern) && !code.includes(replacement)) {
        return code.replace(pattern, replacement)
      }
    },
  }
}

export default defineConfig({
  plugins: [vue(), staticAssetResolver(), vueRenderSlotNullGuard()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'web/src'),
    },
  },
  publicDir: resolve(__dirname, 'assets'),
  test: {
    environment: 'jsdom',
    css: true,
    // Detect async resource leaks (unclosed timers, sockets) in test files.
    // Helps identify which tests cause worker processes to hang on exit.
    detectAsyncLeaks: true,
    // Teardown timeout: vitest waits this long for workers to close after
    // tests finish. If workers have open handles (Vite server FILEHANDLEs,
    // vue-i18n enableDevTools promise), they can't exit cleanly.
    // Set to 5s (reduced from default 10s) as a balance between waiting
    // for normal cleanup and not blocking too long on hung workers.
    // The globalSetup force-exit timer (2s) fires after pool cleanup.
    teardownTimeout: 5_000,
    // Force-exit safety net for vitest 4.x pool cleanup hang bug.
    // See vitest-dev/vitest#8766, #9494, #8861, #9123.
    globalSetup: [resolve(__dirname, 'vitest-globalSetup.ts')],
    // 'hanging-process' reporter warns when tests leave open handles
    // (timers, sockets, etc.) that prevent worker processes from exiting.
    reporters: ['default', 'hanging-process'],
    // Use 'forks' pool. Vitest 4.x has a known bug where fork workers can
    // become zombies on pool cleanup (vitest-dev/vitest#8766). Mitigated by:
    // - vitest-globalSetup.ts: force-exit timer + orphan worker kill
    // - scripts/vitest-run.sh: watchdog timeout + process tree kill
    // We use 'forks' (not 'threads') because with threads, open handles
    // (setInterval, addEventListener) in test components keep the shared
    // main process event loop alive, preventing globalSetup teardown() from
    // ever running. With forks, teardown() runs in the main process even if
    // worker child processes hang.
    pool: 'forks',
    maxWorkers: 4,
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
      provider: 'v8',
      reporter: ['text', 'json', 'json-summary'],
    },
    setupFiles: [resolve(__dirname, 'web/src/test-setup.ts')],
  },
})

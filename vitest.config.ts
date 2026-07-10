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
    // Reduce teardown timeout from default 10s to 5s so vitest's own
    // safety net (exit() method's unref'd timer) fires sooner.
    teardownTimeout: 5_000,
    // Force-exit safety net for vitest 4.x pool cleanup hang bug.
    // See vitest-dev/vitest#8766, #9494, #8861, #9123.
    globalSetup: [resolve(__dirname, 'vitest-globalSetup.ts')],
    // 'hanging-process' reporter warns when tests leave open handles
    // (timers, sockets, etc.) that prevent worker processes from exiting.
    reporters: ['default', 'hanging-process'],
    // Limit fork workers to 4 to reduce zombie accumulation on high-core
    // machines. Default is CPU count (16 here), which spawns too many
    // workers when pool cleanup hangs.
    // Note: In Vitest 4, poolOptions was removed; maxWorkers is now
    // top-level. minWorkers was removed (only maxWorkers has effect).
    // See: https://vitest.dev/guide/migration#pool-rework
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
      reporter: ['text', 'json', 'json-summary'],
    },
    setupFiles: [resolve(__dirname, 'web/src/test-setup.ts')],
  },
})

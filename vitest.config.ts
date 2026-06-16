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

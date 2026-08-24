import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'path'

// Independent Excalidraw build.
//
// This bundle is intentionally NOT part of the Vue app's main build: it brings
// React + @excalidraw/excalidraw (~5MB raw / ~1.7MB gzip) which must never
// bloat the main chunk. Output goes to web/public/vendor/excalidraw/ so it is
// served at /vendor/excalidraw/index.html by the backend (ServeIndex →
// frontend.GetFS()), embedded into the single binary via build.sh, and lazy
// loaded by the Vue app through an <iframe> + postMessage bridge.
export default defineConfig({
  plugins: [react()],
  root: '.',
  // The host page lives under /vendor/excalidraw/, so asset URLs must be
  // prefixed accordingly. Without this, index.html references /assets/* at the
  // site root → 404 (and, in the sandboxed iframe, CORS-blocked).
  base: '/vendor/excalidraw/',
  build: {
    // Output into the ROOT public/ dir (same as the main Vue build's outDir).
    // build.sh copies public/ → internal/frontend/dist → go:embed, so anything
    // placed here ships with the single binary and is served at
    // /vendor/excalidraw/index.html by ServeIndex.
    outDir: resolve(__dirname, '../../../public/vendor/excalidraw'),
    emptyOutDir: true,
    // Keep hashed assets flat; index.html is the only entry the iframe needs.
    assetsDir: 'assets',
    rollupOptions: {
      output: {
        manualChunks(id) {
          // Split React into its own cacheable chunk.
          if (id.includes('/react/') || id.includes('react-dom')) return 'vendor-react'
          return undefined
        },
      },
    },
  },
})

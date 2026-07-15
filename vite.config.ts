import { defineConfig, Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import VueI18nPlugin from '@intlify/unplugin-vue-i18n/vite'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'
import { cpSync, existsSync, mkdirSync, readdirSync } from 'fs'

const __dirname = dirname(fileURLToPath(import.meta.url))
const publicDir = resolve(__dirname, 'public')
const srcAssets = resolve(__dirname, 'assets')

// Ensure public/ exists
if (!existsSync(publicDir)) mkdirSync(publicDir, { recursive: true })

// Copy logo files to public/ so they are served at /assets/*
if (existsSync(srcAssets)) {
  // Ensure public/assets directory exists
  const publicAssets = resolve(publicDir, 'assets')
  if (!existsSync(publicAssets)) mkdirSync(publicAssets, { recursive: true })

  for (const f of readdirSync(srcAssets)) {
    cpSync(resolve(srcAssets, f), resolve(publicAssets, f), { force: true })
  }
}

// Vite plugin: copy material-icon-theme SVGs to web/src/assets/material-icons/
// so import.meta.glob can reference them (Vite cannot glob into node_modules).
function materialIconsCopy(): Plugin {
  const srcDir = resolve(__dirname, 'node_modules/material-icon-theme/icons')
  const destDir = resolve(__dirname, 'web/src/assets/material-icons')

  function copy() {
    if (!existsSync(srcDir)) {
      console.warn('[materialIconsCopy] Source not found:', srcDir)
      return
    }
    mkdirSync(destDir, { recursive: true })
    for (const f of readdirSync(srcDir)) {
      if (f.endsWith('.svg')) {
        cpSync(resolve(srcDir, f), resolve(destDir, f), { force: true })
      }
    }
  }

  return {
    name: 'material-icons-copy',
    buildStart() { copy() },
    configureServer() { copy() },
  }
}

// Vite plugin: wrap highlight.js theme CSS with attribute selectors
// so light/dark themes can coexist without conflict.
function hljsThemeWrapper(): Plugin {
  return {
    name: 'hljs-theme-wrapper',
    transform(code: string, id: string) {
      if (!id.includes('highlight.js/styles/')) return null
      const theme = id.endsWith('github-dark.css') ? 'dark' : 'light'
      // Wrap all top-level .hljs-* rules with [data-hljs-theme="..."]
      const wrapped = code.replace(
        /^(\.[a-z-]+\s*\{)/gm,
        `[data-hljs-theme="${theme}"] $1`
      )
      return { code: wrapped, map: null }
    },
  }
}

const backendPort = process.env.VITE_BACKEND_PORT || 20000
const backendProto = process.env.VITE_BACKEND_PROTO || 'https'
const frontendPort = parseInt(process.env.VITE_FRONTEND_PORT || '20001', 10)

export default defineConfig({
  plugins: [
    vue(),
    VueI18nPlugin({
      include: resolve(__dirname, 'web/src/i18n/locales/**'),
      strictMessage: false,
    }),
    hljsThemeWrapper(),
    materialIconsCopy()
  ],
  root: 'web',
  publicDir: srcAssets,
  server: {
    host: process.env.VITE_HOST || '0.0.0.0',
    allowedHosts: ['xulongzhe.top', 'your-domain.com', 'localhost', '127.0.0.1'],
    port: frontendPort,
    proxy: {
      '/api/terminal/ws': {
        target: `${backendProto === 'https' ? 'wss' : 'ws'}://localhost:${backendPort}`,
        ws: true,
        secure: false,
      },
      '/api': {
        target: `${backendProto}://localhost:${backendPort}`,
        secure: false,
        // Don't buffer SSE responses - needed for streaming chat
        configure: (proxy) => {
          proxy.on('proxyRes', (proxyRes) => {
            if (proxyRes.headers['content-type'] === 'text/event-stream') {
              proxyRes.headers['cache-control'] = 'no-cache'
              proxyRes.headers['x-accel-buffering'] = 'no'
            }
          })
        },
      },
      '/login': `${backendProto}://localhost:${backendPort}`,
      '/dialog': `${backendProto}://localhost:${backendPort}`,
      '/assets': `${backendProto}://localhost:${backendPort}`,
      '/sw.js': `${backendProto}://localhost:${backendPort}`,
      '/manifest.json': `${backendProto}://localhost:${backendPort}`,
    },
  },
  build: {
    outDir: publicDir,
    emptyOutDir: false,
    assetsDir: '.',
    rollupOptions: {
      input: resolve(__dirname, 'web/index.html'),
    },
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'web/src'),
    },
  },
})

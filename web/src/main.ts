import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import { LongPressDirective } from './directives/longPress.ts'
import { configureMarkedRenderer } from './utils/markedConfig.ts'
import { appLog } from './utils/appLog.ts'

configureMarkedRenderer()

const app = createApp(App)
app.use(i18n)
app.directive('long-press', LongPressDirective)

// Capture Vue component errors (render, lifecycle, event handlers)
app.config.errorHandler = (err, instance, info) => {
  const msg = err instanceof Error ? (err.stack || err.message) : String(err)
  appLog.e('Vue', `${msg} [${info}]`)
}

// Capture uncaught JS errors (non-Vue)
window.addEventListener('error', (e) => {
  appLog.e('JS.Uncaught', `${e.message} at ${e.filename}:${e.lineno}`)
})

// Capture resource loading failures (img/script/link 404s, etc.)
// Resource error events fire on the element and do NOT bubble, so capture phase is required.
window.addEventListener('error', (e) => {
  if (e.target && e.target !== window) {
    const el = e.target as HTMLElement
    const tag = el.tagName || '?'
    const src = (el as HTMLImageElement).src || (el as HTMLLinkElement).href || ''
    if (src) {
      appLog.e('JS.Resource', `Failed to load <${tag}> src=${src}`)
    }
  }
}, true)

// Capture unhandled Promise rejections with improved serialization
window.addEventListener('unhandledrejection', (e) => {
  const r = e.reason
  const msg = r instanceof Error ? (r.stack || r.message) :
    (typeof r === 'object' && r ? JSON.stringify(r) : String(r))
  appLog.e('JS.Promise', msg)
})

app.mount('#app')

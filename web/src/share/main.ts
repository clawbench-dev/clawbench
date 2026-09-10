import { installPromiseWithResolversPolyfill } from '../utils/polyfills'

// Must run before any module that depends on Promise.withResolvers (e.g. pdfjs-dist).
installPromiseWithResolversPolyfill()

// Global styles. Referenced from share.html via <link> too, but importing them
// here guarantees they are bundled into this entry's own CSS output (Vite only
// emits shared <link>-referenced CSS into the first HTML entry).
import '../../css/variables.css'
import '../../css/base.css'
import '../../css/layout.css'
import '../../css/wide-screen.css'
import '../../css/markdown-common.css'
import '../../css/code-block.css'
import '../../css/code-block-header.css'
import '../../css/content.css'
import '../../css/media-block.css'
import '../../css/components.css'
// Share chrome (topbar/body/TOC rail) — single shared source also used by the
// markdown HTML export (exportMarkdownHtml.ts embeds the same file).
import '../../css/share-chrome.css'

import { createApp } from 'vue'
import ShareView from './ShareView.vue'
import i18n from '../i18n'
import { LongPressDirective } from '../directives/longPress'
import { configureMarkedRenderer } from '../utils/markedConfig'

configureMarkedRenderer()

const app = createApp(ShareView)
app.use(i18n)
app.directive('long-press', LongPressDirective)
app.mount('#app')

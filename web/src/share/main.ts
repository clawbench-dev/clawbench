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
// Shared diff-row tint + leading rail (git history, diff drawer, chat tool-call
// edits, CodeMirror changed-line overlay). The share SPA renders markdown/code
// through the same components, so it needs the same rules.
import '../../css/diff-rows.css'
// Share chrome (topbar/body/TOC rail) — single shared source also used by the
// markdown HTML export (exportMarkdownHtml.ts embeds the same file).
import '../../css/share-chrome.css'

import { createApp } from 'vue'
import ShareRoot from './ShareRoot.vue'
import i18n from '../i18n'
import { LongPressDirective } from '../directives/longPress'
import { configureMarkedRenderer } from '../utils/markedConfig'
import { installLocalMediaFallback } from '../utils/localMediaFallback'
import { installMediaFullscreenGuard } from '../utils/mediaFullscreenGuard'
import { installExternalLinkClick } from '../utils/externalLinkClick'

configureMarkedRenderer()

// A shared document's local images are served through the token-scoped
// /api/share/{token}/local/ endpoint; a deleted or out-of-scope file answers
// 404. Show the same labelled placeholder the main app uses instead of a
// silent broken image (issue #501).
installLocalMediaFallback()

// The share SPA renders the same VideoPreview, so a fullscreen video whose file
// disappears would otherwise leave the page unclickable (see the guard's docs).
installMediaFullscreenGuard()

// External links are rendered as <span class="md-external-link"> by the shared
// markdown renderer, so this SPA needs the tap handler too — otherwise a URL in
// a shared document would be dead text. The layer is installed once and never
// removed: this entry lives for the whole page's lifetime.
installExternalLinkClick()

const app = createApp(ShareRoot)
app.use(i18n)
app.directive('long-press', LongPressDirective)
app.mount('#app')

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

// ── Global component styles the main app gets from App.vue / index.html ──
//
// These are NOT `@import`ed by anything above, and share.html has no <link>
// tags (every stylesheet reaches this entry through JS). Anything the app loads
// only from App.vue or index.html therefore has to be listed HERE as well, or
// the share page renders that component with raw UA styling.
//
// The failure mode is silent and easy to miss: the elements exist, the classes
// match, and only the geometry is wrong (a `<button>` keeps its 2px outset
// border and 18×21 UA box instead of the app's 22px borderless chip). Keep this
// list in sync with App.vue's CSS imports — `shareStylesheetParity.test.ts`
// fails when they diverge.
//
// Meta bar buttons (copy / details / summary / speak). `.chat-action-btn` is
// defined ONLY here; without it every action under a shared message renders as
// a default browser button.
import '../assets/chat-actions.css'
// File-path / commit-hash / localhost / worktree chips inside tool output. The
// composables inject these classes into `v-html`, so they must be global; the
// share page renders the same annotated markup.
import '../assets/annotation-buttons.css'
// Copy-button "copied" feedback (`.copy-btn.is-copied`). index.html links it
// for the app; the share entry has to import it.
import '../../css/copy-button.css'
// Code-block syntax colours. `highlightCode` emits `<span class="hljs-*">`, so
// without the two themes a fenced block on a shared page is plain monochrome
// text — the markup is highlighted, the colours simply never load. The light
// override tightens contrast against the app's --code-bg.
import 'highlight.js/styles/github.css'
import 'highlight.js/styles/github-dark.css'
import '../assets/hljs-light-override.css'
// Monochrome agent icons (`mono-opencode`, `mono-cursor`, …). AgentIcon sets the
// class from the backend; on a dark theme several of those fills are near-black
// without this sheet, so the avatar in the share topbar disappears.
import '../assets/mono-icon-colors.css'

import { createApp } from 'vue'
import ShareRoot from './ShareRoot.vue'
import i18n from '../i18n'
import { LongPressDirective } from '../directives/longPress'
import { configureMarkedRenderer } from '../utils/markedConfig'
import { installLocalMediaFallback } from '../utils/localMediaFallback'
import { installMediaFullscreenGuard } from '../utils/mediaFullscreenGuard'
import { installInlineZoom } from '../utils/inlineZoom'
import { installSvgMediaAutoFit } from '../utils/svgMediaFit'
import { installExternalLinkClick } from '../utils/externalLinkClick'
// Follow theme switches made from the share topbar: updates the reactive theme
// ref (iframe-hosted viewers read it through provide/inject) and re-renders
// mermaid diagrams, whose colours are baked into the SVG at render time. The
// app does both from App.vue; the share SPA mounts no App.vue, so it must wire
// them itself or a switch would leave diagrams and Swagger docs on the old
// palette.
import { installShareThemeSync } from './shareTheme'
import { startSystemThemeWatcher } from '../composables/useSettingsConfig'

configureMarkedRenderer()

// A shared document's local images are served through the token-scoped
// /api/share/{token}/local/ endpoint; a deleted or out-of-scope file answers
// 404. Show the same labelled placeholder the main app uses instead of a
// silent broken image (issue #501).
installLocalMediaFallback()

// The share SPA renders the same VideoPreview, so a fullscreen video whose file
// disappears would otherwise leave the page unclickable (see the guard's docs).
installMediaFullscreenGuard()
installInlineZoom()
installSvgMediaAutoFit()

// External links are rendered as <span class="md-external-link"> by the shared
// markdown renderer, so this SPA needs the tap handler too — otherwise a URL in
// a shared document would be dead text. The layer is installed once and never
// removed: this entry lives for the whole page's lifetime.
installExternalLinkClick()

installShareThemeSync()

// Follow the system light/dark preference while the stored choice is 'auto',
// exactly as the app does. Without this, picking "System" in the share topbar
// would resolve once at load and then never react to the OS flipping scheme —
// a visible gap in the very feature the picker offers.
startSystemThemeWatcher()

const app = createApp(ShareRoot)
app.use(i18n)
app.directive('long-press', LongPressDirective)
app.mount('#app')

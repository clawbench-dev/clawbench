/**
 * highlight.js facade: eager core + lazily-loaded long tail.
 *
 * `highlightCode` is SYNCHRONOUS and stays that way — it is called from
 * template render functions all over the app, and making it async would
 * cascade into every markdown render path. A language that has not been
 * loaded yet falls back to plain text (HTML-escaped, no colour).
 *
 * Callers that can afford to wait (a file preview that already does an async
 * fetch, a component that re-renders on data) should `await ensureLanguage()`
 * first so the first paint is already highlighted instead of flashing plain
 * text. `onLanguageLoaded` lets a caller that cannot await re-render once the
 * grammar arrives.
 *
 * The eager set is deliberately the ~56 languages a chat message or a code
 * preview realistically needs; everything else (protobuf, nix, julia, …) lives
 * in `highlightExtra.ts` behind one dynamic import each. See that file's doc
 * for why the split exists.
 */

import { shallowRef } from 'vue'
import { hljs, EAGER_LANGUAGES } from './highlightCore.ts'
import { LAZY_LOADERS, LAZY_ALIASES } from './highlightExtra.ts'

export { hljs, EAGER_LANGUAGES }

/**
 * Bumped every time a lazy grammar finishes loading.
 *
 * `highlightCode` is synchronous, so a render that ran before its grammar
 * arrived produced plain text and will keep producing it. A `computed` (or a
 * cached HTML string) that reads `languageVersion.value` re-evaluates on the
 * bump, which is how the code preview and the chat renderer pick up colour
 * without threading an await through the template.
 */
export const languageVersion = shallowRef(0)

/** In-flight and settled loads, so concurrent callers share one import. */
const pending = new Map<string, Promise<boolean>>()

/** Subscribers notified after any grammar finishes loading. */
const loadedListeners = new Set<(language: string) => void>()

/**
 * Languages already known to have no grammar. Without this, every render of
 * every unlabelled/unknown block would re-enter `ensureLanguage` and allocate
 * a fresh resolved promise — hundreds per second while a turn streams.
 */
const knownMissing = new Set<string>()

/**
 * Resolve a language name to the key used by the loaders.
 *
 * Tries the name as given, then our own lazy-only aliases (`proto` →
 * protobuf). Returns null when nothing can be fetched — either because it is
 * already registered, or because no grammar exists.
 */
function resolveLoaderKey(language: string): string | null {
    if (LAZY_LOADERS[language]) return language
    if (LAZY_ALIASES[language]) return LAZY_ALIASES[language]
    return null
}

/**
 * Load `language` if it is not registered yet.
 *
 * Resolves `true` when the language is available afterwards (already eager,
 * already loaded, or just fetched) and `false` when no grammar exists for it.
 * Never rejects: a failed chunk fetch degrades to plain text rather than
 * breaking the caller's render.
 */
export function ensureLanguage(language: string): Promise<boolean> {
    if (!language) return Promise.resolve(false)
    if (hljs.getLanguage(language)) return Promise.resolve(true)
    if (knownMissing.has(language)) return Promise.resolve(false)

    const key = resolveLoaderKey(language)
    if (!key) {
        knownMissing.add(language)
        return Promise.resolve(false)
    }

    const cached = pending.get(key)
    if (cached) return cached

    const loader = LAZY_LOADERS[key]
    if (!loader) {
        knownMissing.add(language)
        return Promise.resolve(false)
    }

    const task = loader()
        .then((grammar) => {
            // Guard against a double registration when two aliases race to the
            // same grammar: registerLanguage is idempotent in practice, but the
            // alias table is not, and re-registering resets it.
            if (!hljs.getLanguage(key)) hljs.registerLanguage(key, grammar as never)
            for (const [alias, target] of Object.entries(LAZY_ALIASES)) {
                if (target === key) hljs.registerAliases(alias, { languageName: key })
            }
            return hljs.getLanguage(language) ? true : hljs.getLanguage(key) !== undefined
        })
        .catch(() => false)
        .then((ok) => {
            pending.delete(key)
            if (ok) {
                languageVersion.value++
                for (const listener of loadedListeners) {
                    try {
                        listener(language)
                    } catch {
                        // A broken listener must not fail the load for everyone else.
                    }
                }
            }
            return ok
        })

    pending.set(key, task)
    return task
}

/**
 * Subscribe to grammar-load completions.
 *
 * Callers that render synchronously (markdown blocks, template helpers) use
 * this to schedule exactly one re-render after the grammar they asked for
 * arrives. Returns an unsubscribe function.
 */
export function onLanguageLoaded(listener: (language: string) => void): () => void {
    loadedListeners.add(listener)
    return () => loadedListeners.delete(listener)
}

/** True when `language` can be highlighted right now (no lazy load pending). */
export function isLanguageReady(language: string): boolean {
    return !!language && !!hljs.getLanguage(language)
}

/**
 * Highlight code with a specific language.
 * Falls back to plain text (no highlighting) for unregistered languages.
 *
 * Synchronous by design — `await ensureLanguage(language)` first if the
 * language may live in the lazy tier and you want colour on the first paint.
 *
 * When the language is in the lazy tier but not loaded yet, this schedules the
 * load and returns plain text. That is deliberate: the alternative is every
 * render path (markdown blocks, diff rows, search results, file previews)
 * threading an await through code that is called from templates. Callers that
 * can re-render subscribe to `onLanguageLoaded` and get colour on the second
 * pass, a few hundred milliseconds later.
 */
export function highlightCode(code: string, language: string, ignoreIllegals = true): string {
    if (language && hljs.getLanguage(language)) {
        return hljs.highlight(code, { language, ignoreIllegals }).value
    }
    // Not loaded yet, but a grammar exists: warm it so the next render has it.
    // `ensureLanguage` memoizes misses and in-flight loads, so a render loop
    // hitting the same unknown language does not allocate a promise per frame.
    if (language) void ensureLanguage(language)
    // Fallback: no highlighting for unregistered languages
    return escapeHtml(code)
}

function escapeHtml(text: string): string {
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
}

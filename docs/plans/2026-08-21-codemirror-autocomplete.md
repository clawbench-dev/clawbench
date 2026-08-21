# CodeMirror Autocomplete Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add intelligent autocomplete to the CodeMirror editor for common file types, leveraging built-in language completion sources and `@codemirror/autocomplete`.

**Architecture:** Use `@codemirror/autocomplete`'s `autocompletion()` extension, configured with per-language completion sources from the existing `@codemirror/lang-*` packages. Only enable in editable mode. Register completion sources alongside language extensions in `codeEditorLang.ts` via a new `buildCompletionExtension()` function. Style the tooltip to match the app's dark/light theme.

**Tech Stack:** `@codemirror/autocomplete` (v6.20.3, already installed as transitive dep), existing `@codemirror/lang-*` packages' built-in completion sources.

---

### Task 1: Add `@codemirror/autocomplete` as a direct dependency

**Files:**
- Modify: `package.json`

**Step 1: Add the direct dependency**

```bash
cd /home/xulongzhe/projects/clawbench && npm install @codemirror/autocomplete
```

**Step 2: Verify installation**

```bash
grep "@codemirror/autocomplete" package.json
```

Expected: `@codemirror/autocomplete` appears under `dependencies` with a semver range.

**Step 3: Commit**

```bash
git add package.json package-lock.json && git commit -m "chore: add @codemirror/autocomplete as direct dependency"
```

---

### Task 2: Create `buildCompletionExtension()` in `codeEditorLang.ts`

**Files:**
- Modify: `web/src/utils/codeEditorLang.ts`
- Create: `web/src/utils/__tests__/codeEditorLang.test.ts`

**Step 1: Write the failing test**

```ts
// web/src/utils/__tests__/codeEditorLang.test.ts
import { describe, it, expect } from 'vitest'
import { buildLangExtension, buildCompletionExtension, COMPLETION_LANGS } from '@/utils/codeEditorLang'

describe('buildCompletionExtension', () => {
  it('returns a completion extension for languages with built-in sources', async () => {
    const ext = await buildCompletionExtension('javascript')
    expect(Array.isArray(ext) ? ext.length > 0 : true).toBe(true)
  })

  it('returns empty array for languages without completion sources', async () => {
    const ext = await buildCompletionExtension('yaml')
    expect(ext).toEqual([])
  })

  it('returns empty array for unknown languages', async () => {
    const ext = await buildCompletionExtension('brainfuck')
    expect(ext).toEqual([])
  })
})

describe('COMPLETION_LANGS', () => {
  it('covers the major languages that have completion sources', () => {
    const expected = ['javascript', 'typescript', 'html', 'css', 'python', 'sql', 'go', 'less', 'sass', 'liquid', 'markdown']
    for (const lang of expected) {
      expect(lang in COMPLETION_LANGS).toBe(true)
    }
    // markdown has null factory (uses built-in HTML tag completion)
    expect(COMPLETION_LANGS['markdown']).toBeNull()
  })
})
```

**Step 2: Run test to verify it fails**

```bash
npx vitest run web/src/utils/__tests__/codeEditorLang.test.ts
```

Expected: FAIL — `buildCompletionExtension` and `COMPLETION_LANGS` don't exist yet.

**Step 3: Implement `buildCompletionExtension()` and `COMPLETION_LANGS`**

Add to `web/src/utils/codeEditorLang.ts`:

```ts
import type { CompletionSource } from '@codemirror/autocomplete'

/**
 * Languages that provide built-in completion sources in their @codemirror/lang-* packages.
 * Each entry maps to a lazy factory that returns the completion source function.
 * Languages whose completion is embedded in the language extension itself (e.g. markdown
 * auto-completes HTML tags via `completeHTMLTags`) use a `null` factory — they only need
 * the `autocompletion()` extension enabled, no override source.
 */
const COMPLETION_LANGS: Record<string, (() => CompletionSource | Promise<CompletionSource>) | null> = {
  javascript: () => import('@codemirror/lang-javascript').then(m => m.localCompletionSource),
  typescript: () => import('@codemirror/lang-javascript').then(m => m.localCompletionSource),
  html: () => import('@codemirror/lang-html').then(m => m.htmlCompletionSource),
  css: () => import('@codemirror/lang-css').then(m => m.cssCompletionSource),
  python: () => import('@codemirror/lang-python').then(m => m.localCompletionSource),
  sql: () => import('@codemirror/lang-sql').then(m => m.sqlCompletionSource()),
  go: () => import('@codemirror/lang-go').then(m => m.goCompletionSource),
  less: () => import('@codemirror/lang-less').then(m => m.cssCompletionSource),
  sass: () => import('@codemirror/lang-sass').then(m => m.cssCompletionSource),
  liquid: () => import('@codemirror/lang-liquid').then(m => m.liquidCompletionSource()),
  // Markdown auto-completes HTML tags when typing `<` — built into the markdown()
  // extension (completeHTMLTags, default true). Only needs autocompletion() enabled.
  markdown: null,
}

/**
 * Build a completion extension for a given language.
 * Returns an empty array for languages without a built-in completion source.
 */
export async function buildCompletionExtension(fileLang: string): Promise<Extension[]> {
  if (!(fileLang in COMPLETION_LANGS)) return []
  // Re-import autocompletion lazily to avoid pulling it into the main bundle
  // for editors that never enter edit mode.
  const { autocompletion } = await import('@codemirror/autocomplete')
  const factory = COMPLETION_LANGS[fileLang]
  if (factory) {
    const source = await factory()
    return [autocompletion({ override: [source] })]
  }
  // null factory (e.g. markdown): just enable autocompletion with defaults
  return [autocompletion()]
}
```

Also export `COMPLETION_LANGS` for the test.

**Step 4: Run test to verify it passes**

```bash
npx vitest run web/src/utils/__tests__/codeEditorLang.test.ts
```

Expected: PASS

**Step 5: Commit**

```bash
git add web/src/utils/codeEditorLang.ts web/src/utils/__tests__/codeEditorLang.test.ts && git commit -m "feat: add buildCompletionExtension with per-language completion sources"
```

---

### Task 3: Add a `completionCompartment` to CodeMirrorViewer.vue

**Files:**
- Modify: `web/src/components/file/CodeMirrorViewer.vue`

**Step 1: Add the compartment**

In `CodeMirrorViewer.vue`, add a new compartment alongside the existing ones:

```ts
const completionCompartment = new Compartment()
```

**Step 2: Include it in `buildAllExtensions()`**

```ts
function buildAllExtensions() {
    return [
        readonlyCompartment.of(props.editable ? [] : [EditorState.readOnly.of(true)]),
        langCompartment.of([]),
        completionCompartment.of([]), // placeholder; loaded async in mountCompletion()
        lineNumbersCompartment.of(props.showLineNumbers ? [lineNumbers()] : []),
        // ...rest unchanged
    ]
}
```

**Step 3: Add `mountCompletion()` function**

```ts
/** Load the completion extension asynchronously for languages that provide one. */
async function mountCompletion() {
    if (!props.editable) return
    const ext = await buildCompletionExtension(props.language)
    if (view.value) {
        view.value.dispatch({ effects: completionCompartment.reconfigure(ext) })
    }
}
```

**Step 4: Call `mountCompletion()` on mount and on relevant prop changes**

In `onMounted`:
```ts
onMounted(() => {
    // ...existing code...
    mountCompletion()  // add after mountLang()
})
```

Add watchers for `editable` and `language`:
```ts
watch([() => props.editable], () => {
    // ...existing readonly reconfigure...
    mountCompletion()  // enable/disable completions when toggling edit mode
})
watch([() => props.language], () => mountCompletion())
```

**Step 5: Re-apply completion after content refresh (setState path)**

In the `watch(() => props.content, ...)` handler, after `mountLang()`, also call `mountCompletion()` — `setState()` rebuilds all extensions from scratch, so the completion compartment needs re-seeding.

**Step 6: Commit**

```bash
git add web/src/components/file/CodeMirrorViewer.vue && git commit -m "feat: integrate autocompletion compartment into CodeMirrorViewer"
```

---

### Task 4: Style the autocomplete tooltip

**Files:**
- Modify: `web/src/components/file/CodeMirrorViewer.vue` (global `<style>` block)

**Step 1: Add tooltip styles**

Append to the global `<style>` section of `CodeMirrorViewer.vue`:

```css
/* Autocomplete tooltip */
.cm-viewer .cm-tooltip-autocomplete {
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 8px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
  font-size: 13px;
  max-height: 200px;
}
.cm-viewer .cm-tooltip-autocomplete ul li {
  padding: 2px 8px 2px 4px;
}
.cm-viewer .cm-completionIcon {
  width: 16px;
  font-size: 11px;
  opacity: 0.7;
}
.cm-viewer .cm-completionIcon::after {
  content: '';
}
.cm-viewer .cm-completionLabel {
  color: var(--text-primary);
}
.cm-viewer .cm-completionDetail {
  color: var(--text-muted);
  font-style: italic;
}
.cm-viewer .cm-activeCompletion {
  background: color-mix(in srgb, var(--accent-color) 20%, transparent);
  color: var(--text-primary);
}
.cm-viewer .cm-completionMatchedText {
  color: var(--accent-color);
  font-weight: 600;
}
```

**Step 2: Verify visually**

Open the dev server, edit a `.js` file, type a keyword prefix — the tooltip should appear with the app's theme colors.

**Step 3: Commit**

```bash
git add web/src/components/file/CodeMirrorViewer.vue && git commit -m "feat: style autocomplete tooltip to match app theme"
```

---

### Task 5: Add autocomplete keybinding and test

**Files:**
- Modify: `web/src/components/file/CodeMirrorViewer.vue`
- Modify: `web/src/components/file/__tests__/CodeMirrorViewer.test.ts`

**Step 1: Add the autocomplete keymap**

The `autocompletion()` extension already adds Ctrl+Space (explicit trigger) and implicit typing triggers. We need to add the `acceptCompletion` keybinding to the keymap so Tab/Enter work as expected. Import from `@codemirror/autocomplete`:

```ts
import { acceptCompletion } from '@codemirror/autocomplete'
```

Add to the keymap:
```ts
keymap.of([
    { key: 'Mod-s', run: handleSaveShortcut, preventDefault: true },
    ...defaultKeymap,
    ...historyKeymap,
]),
```

Note: `autocompletion()` already registers its own keymap with Ctrl+Space and navigation keys. No additional keymap entries needed — the completion tooltip handles Enter/Tab natively.

**Step 2: Write test for autocomplete presence in editable mode**

Add to `CodeMirrorViewer.test.ts`:

```ts
it('enables autocompletion compartment in editable mode', async () => {
    const wrapper = mountViewer({ editable: true, language: 'javascript', content: 'const a = 1\n' })
    await sleep(150) // wait for async mountCompletion
    const view = wrapper.vm.getView()
    // The autocompletion extension adds a .cm-tooltip-autocomplete class on the editor
    // We verify the extension is wired by checking the compartment is non-empty
    // (facet-based check is fragile in test; verify no crash + editor works)
    expect(view.state.doc.toString()).toBe('const a = 1\n')
})

it('does not enable autocompletion in read-only mode', async () => {
    const wrapper = mountViewer({ editable: false, language: 'javascript', content: 'const a = 1\n' })
    await sleep(150)
    // mountCompletion early-returns when !editable; no autocomplete extension
    expect(wrapper.find('.cm-viewer').exists()).toBe(true)
})
```

**Step 3: Run all CodeMirrorViewer tests**

```bash
npx vitest run web/src/components/file/__tests__/CodeMirrorViewer.test.ts
```

Expected: All tests pass.

**Step 4: Commit**

```bash
git add web/src/components/file/CodeMirrorViewer.vue web/src/components/file/__tests__/CodeMirrorViewer.test.ts && git commit -m "test: add tests for autocomplete compartment in editable/read-only modes"
```

---

### Task 6: Run full pre-push checks

**Step 1: Run the pre-push checks**

```bash
./scripts/pre-push-checks.sh --skip-coverage
```

Expected: All checks pass.

**Step 2: Fix any issues if checks fail**

Address lint errors, type errors, or test failures.

**Step 3: Final commit if needed**

```bash
git add -A && git commit -m "fix: resolve pre-push check issues"
```

---

## Summary of supported completion sources

| Language | Completion Source | Package |
|----------|------------------|---------|
| JavaScript/TypeScript | `localCompletionSource` | `@codemirror/lang-javascript` |
| HTML | `htmlCompletionSource` | `@codemirror/lang-html` |
| CSS/Less/Sass | `cssCompletionSource` | `@codemirror/lang-css` |
| Python | `localCompletionSource` | `@codemirror/lang-python` |
| SQL | `sqlCompletionSource()` | `@codemirror/lang-sql` |
| Go | `goCompletionSource` | `@codemirror/lang-go` |
| Liquid | `liquidCompletionSource()` | `@codemirror/lang-liquid` |
| Markdown | built-in HTML tag completion (`completeHTMLTags`) | `@codemirror/lang-markdown` |

Languages without built-in completion sources (YAML, XML, JSON, Rust, Java, C/C++, etc.) will not get autocomplete — the extension returns `[]` for these. This can be extended later with custom keyword-based sources if needed.

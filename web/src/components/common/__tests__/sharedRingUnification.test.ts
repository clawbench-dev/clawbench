import { describe, expect, it } from 'vitest'

/**
 * Guards for the shared loading ring.
 *
 * The ring (.li-spinner) is declared ONCE in css/components.css, not in
 * LoadingIndicator's <style scoped> block, because three of its callers cannot
 * render a Vue component:
 *
 *   - chat tool-call body     → injected as an HTML string (v-html)
 *   - mermaid diagram loading → injected via innerHTML from a util
 *   - localhost open button   → used to paint from a ::after pseudo-element
 *
 * Injected DOM carries no `data-v-*` attribute, so a scoped rule silently
 * misses it (design-guide red line 1). These tests pin both halves of the
 * arrangement: the ring lives globally, and no call site grows its own copy.
 */

async function source(relPath: string): Promise<string> {
  const mod = await import(/* @vite-ignore */ `${relPath}?raw`)
  return typeof mod.default === 'string' ? mod.default : ''
}

const GLOBAL_CSS = '../../../../css/components.css'

describe('shared loading ring', () => {
  it('is declared globally, with its keyframes', async () => {
    const css = await source(GLOBAL_CSS)
    expect(css, 'the ring must be defined globally').toContain('.li-spinner {')
    expect(css, 'the rotation must be global too (scoped keyframes get renamed)').toContain('@keyframes li-spin')
  })

  it('keeps the size tiers in the global definition', async () => {
    const css = await source(GLOBAL_CSS)
    for (const tier of ['size-sm', 'size-md', 'size-lg']) {
      expect(css, `${tier} must stay global`).toContain(`.${tier} .li-spinner`)
    }
  })

  it('does not re-declare the ring inside the component', async () => {
    // If the ring were re-declared in the scoped block, the global copy could
    // drift out of sync — and the injected callers would still use the global
    // one, so the two would silently diverge.
    const src = await source('@/components/common/LoadingIndicator.vue')
    const scopedRaw = src.slice(src.indexOf('<style scoped>'), src.indexOf('</style>', src.indexOf('<style scoped>')))
    // Strip comments first: the block legitimately *mentions* the global ring in
    // prose, and only a real declaration would be a fork.
    const scoped = scopedRaw.replace(/\/\*[\s\S]*?\*\//g, '')
    expect(scoped, 'the ring must not be re-declared in the scoped block').not.toMatch(/^\.li-spinner\s*\{/m)
    expect(scoped, 'nor its keyframes').not.toMatch(/@keyframes\s+li-spin\b/)
    // The wrapper layout legitimately stays scoped.
    expect(scoped).toContain('.loading-indicator')
  })

  it('leaves no per-site keyframes behind', async () => {
    const css = await source(GLOBAL_CSS)
    const files = [
      ['@/assets/mermaid.css', 'mermaid-spin'],
      ['@/assets/code-link-preview.css', 'code-preview-spin'],
      ['@/assets/annotation-buttons.css', 'url-btn-spin'],
      ['@/components/chat/ChatPanelContent.vue', 'tool-call-spin'],
    ] as const
    for (const [file, dead] of files) {
      const src = await source(file)
      expect(src, `${file} must not define @keyframes ${dead}`).not.toContain(`@keyframes ${dead}`)
      expect(src, `${file} must not reference ${dead}`).not.toContain(`animation: ${dead}`)
    }
    expect(css).not.toContain('tool-call-spin')
  })

  it('marks up every migrated site with the shared ring class', async () => {
    // Vue-template sites use a bare span carrying the global class.
    for (const f of ['@/components/file/CodePreviewBody.vue', '@/components/file/MarkdownPreviewBody.vue']) {
      const src = await source(f)
      expect(src, `${f} must use the shared ring`).toContain('class="li-spinner code-preview-spinner"')
      expect(src, `${f} must use the shared ring for expand spinners`).toContain('class="li-spinner code-preview-expand-spinner"')
    }
    // Injected sites put the class in the generated HTML string.
    const tool = await source('@/composables/useToolDetailDrawer.ts')
    expect(tool, 'tool-call loading must inject the shared ring').toMatch(/tool-call-loading"><span class="li-spinner"/)
    const mermaid = await source('@/utils/mermaid.ts')
    expect(mermaid, 'mermaid loading must inject the shared ring').toContain('class="li-spinner mermaid-spinner"')
    const localhost = await source('@/composables/useLocalhostAnnotation.ts')
    expect(localhost, 'the localhost button must carry the shared ring').toContain('li-spinner chat-url-open-btn-spinner')
  })

  it('overrides only size and colour per site, never the ring shape', async () => {
    // Each migrated site may set the --li-* variables; re-declaring width /
    // height / border-radius / animation would fork the implementation again.
    const sites = [
      ['@/assets/code-link-preview.css', 'code-preview-spinner'],
      ['@/assets/code-link-preview.css', 'code-preview-expand-spinner'],
      ['@/assets/mermaid.css', 'mermaid-spinner'],
      ['@/assets/annotation-buttons.css', 'chat-url-open-btn-spinner'],
    ] as const
    for (const [file, cls] of sites) {
      const src = await source(file)
      // Allow a descendant part: the callers scope themselves under a container
      // so they outrank the global .li-spinner instead of tying with it.
      const rule = src.match(new RegExp(`[^\\n{}]*\\.${cls}\\s*\\{([\\s\\S]*?)\\}`))
      expect(rule, `.${cls} rule must exist`).not.toBeNull()
      const body = rule![1]
      // Match a real declaration at the start of a declaration, so `--li-border:`
      // (a legitimate override) is not mistaken for the forked `border:`.
      for (const forked of ['width', 'height', 'border-radius', 'animation']) {
        expect(body, `.${cls} must not fork "${forked}" — the shared ring owns the shape`)
          .not.toMatch(new RegExp(`(^|;)\\s*${forked}\\s*:`))
      }
      // `border:` is legitimate only as the shorthand's absence: the ring derives
      // it from --li-border, so a site must never restate it.
      expect(body, `.${cls} must not restate the border shorthand`)
        .not.toMatch(/(^|;)\s*border\s*:/)
      expect(body, `.${cls} must size via --li-size`).toContain('--li-size')
    }
  })

  it('outranks the global ring instead of tying with it', async () => {
    // `.li-spinner` is a single class (0,1,0). An override written as a single
    // class too would tie, leaving the winner to stylesheet order — the size
    // would silently fall back to 28px if the bundler reordered the chunks.
    // Every override must therefore carry a descendant part.
    const overrides = [
      ['@/assets/code-link-preview.css', 'code-preview-spinner', '.code-preview-status'],
      ['@/assets/code-link-preview.css', 'code-preview-expand-spinner', '.code-preview-expand-loading'],
      ['@/assets/mermaid.css', 'mermaid-spinner', '.mermaid'],
      ['@/assets/annotation-buttons.css', 'chat-url-open-btn-spinner', '.chat-url-open-btn'],
    ] as const
    for (const [file, cls, ancestor] of overrides) {
      const src = await source(file)
      const rule = src.match(new RegExp(`([^\\n{}]*)\\.${cls}\\s*\\{`))
      expect(rule, `.${cls} rule must exist`).not.toBeNull()
      const selector = rule![1].trim()
      expect(
        selector.startsWith(ancestor),
        `.${cls} must be scoped under ${ancestor} so it outranks .li-spinner (got "${selector}")`,
      ).toBe(true)
    }
    // The tool-call override is already compound (.tool-call-loading .li-spinner).
    const cpc = await source('@/components/chat/ChatPanelContent.vue')
    expect(cpc).toMatch(/\.tool-call-loading \.li-spinner\s*\{/)
  })

  it('keeps each site at the speed it shipped with', async () => {
    // The ring's default is 0.8s, but three of the migrated callers ran at
    // 0.6s. Unifying the shape must not silently retime them, so each keeps its
    // own --li-duration.
    const spedUp = [
      ['@/assets/mermaid.css', 'mermaid-spinner'],
      ['@/assets/annotation-buttons.css', 'chat-url-open-btn-spinner'],
    ] as const
    for (const [file, cls] of spedUp) {
      const src = await source(file)
      const rule = src.match(new RegExp(`\\.${cls}\\s*\\{([\\s\\S]*?)\\}`))
      expect(rule, `.${cls} rule must exist`).not.toBeNull()
      expect(rule![1], `.${cls} shipped at 0.6s`).toContain('--li-duration: 0.6s')
    }
    // tool-call lives in a .vue SFC, so read it separately.
    const cpc = await source('@/components/chat/ChatPanelContent.vue')
    const toolRule = cpc.match(/\.tool-call-loading \.li-spinner\s*\{([\s\S]*?)\}/)
    expect(toolRule, 'the tool-call ring rule must exist').not.toBeNull()
    expect(toolRule![1], 'tool-call shipped at 0.6s').toContain('--li-duration: 0.6s')
  })
})

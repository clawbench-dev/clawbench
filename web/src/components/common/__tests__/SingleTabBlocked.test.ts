import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import SingleTabBlocked from '@/components/common/SingleTabBlocked.vue'

/**
 * The single-tab block screen is the one surface rendered BEFORE the app's own
 * theme plumbing runs: main.ts mounts it as a minimal app with no store and no
 * settings, so nothing has applied the user's chosen theme beyond the inline
 * bootstrap in index.html. That makes its colours easy to get wrong in a way
 * no other component would notice.
 *
 * Regression: the button used `var(--accent, #b8bb26)`. `--accent` is declared
 * nowhere in the repo — the token is `--accent-color` (36 theme blocks in
 * css/variables.css) — so the declaration ALWAYS resolved to its fallback, a
 * hard-coded gruvbox green. Every theme, including all the light ones, got that
 * green button. designTokens.css.test.ts catches the class of bug (an
 * undeclared token reference); these assertions pin this specific surface so a
 * future edit cannot quietly reintroduce it.
 */

// Source-sniffing: probe both roots because cwd differs between a bare
// `vitest` run (web/) and scripts/vitest-run.sh (repo root).
function readSource(): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, 'src/components/common/SingleTabBlocked.vue'), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`SingleTabBlocked.vue not found from cwd: ${process.cwd()}`)
}

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      singleTab: { title: 'T', description: 'D', hint: 'H', reload: 'Reload' },
    },
  },
})

describe('SingleTabBlocked — button accent', () => {
  const src = readSource()

  it('references the real accent token, not the undeclared --accent', () => {
    const btn = src.match(/\.single-tab-block__btn\s*\{([^}]*)\}/)
    expect(btn).toBeTruthy()
    expect(btn![1]).toMatch(/var\(\s*--accent-color\b/)
    // The bare name never existed; a `var(--accent, …)` here means the
    // fallback is doing all the work and the theme is ignored.
    expect(btn![1]).not.toMatch(/var\(\s*--accent\s*[,)]/)
  })

  it('keeps a fallback so the screen still works before data-theme is set', () => {
    // --accent-color lives only under [data-theme="…"], which index.html's
    // inline script sets. Dropping the fallback would leave the button with no
    // background at all if that script ever failed or ran late.
    const btn = src.match(/\.single-tab-block__btn\s*\{([^}]*)\}/)
    expect(btn![1]).toMatch(/var\(\s*--accent-color\s*,/)
  })

  it('does not use the old gruvbox green as the fallback', () => {
    // #b8bb26 was the colour that leaked into every theme. It is still a valid
    // gruvbox accent, so it may legitimately appear in a theme block — but not
    // as this button's fallback, where it applies to all 36 themes.
    const btn = src.match(/\.single-tab-block__btn\s*\{([^}]*)\}/)
    expect(btn![1]).not.toMatch(/#b8bb26/i)
  })

  it('renders a button carrying the styled class', () => {
    // The source assertions above are only meaningful if this class is what the
    // component actually puts on screen.
    const wrapper = mount(SingleTabBlocked, { global: { plugins: [i18n] } })
    expect(wrapper.find('button.single-tab-block__btn').exists()).toBe(true)
  })
})

import { describe, expect, it, afterEach } from 'vitest'
import {
  applyShareUIScale,
  isShareFitEnabled,
  resolveShareUIScale,
  setShareFitEnabled,
  shareNarrowQuery,
  SHARE_FIT_KEY,
  SHARE_NARROW_MAX_DESIGN_PX,
} from '@/share/shareScale'
import { computeAutoUIScale } from '@/utils/uiScale'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * "Fit to screen" is a SWITCH on the share page, not an automatic behaviour.
 *
 * The page renders the app's components, and those are all written in `--dp`
 * (= `1rem / 15`), so they scale with the root font size. The reader decides
 * whether that happens: off (the default) keeps the design size, on fits the
 * page to their screen — the same computation the app's Settings "auto fit"
 * button performs.
 *
 * jsdom cannot measure a screen, so the screen is stubbed and the assertion is
 * about the DOM effect: the root font size is `calc(15px * var(--ui-scale))`,
 * so writing `--ui-scale` to <html> IS the page being resized.
 */

const ROOT_ATTR = '--ui-scale'

afterEach(() => {
  document.documentElement.style.removeProperty(ROOT_ATTR)
  document.documentElement.style.removeProperty('--input-line-height')
  document.documentElement.style.zoom = ''
  localStorage.removeItem(SHARE_FIT_KEY)
})

/** Stub the screen so the derivation is deterministic (1440p → 1.33). */
function stubScreen(shortEdge: number, dpr = 1): void {
  Object.defineProperty(window.screen, 'width', { value: shortEdge, configurable: true })
  Object.defineProperty(window.screen, 'height', { value: shortEdge, configurable: true })
  Object.defineProperty(window, 'devicePixelRatio', { value: dpr, configurable: true })
}

describe('the switch is off by default', () => {
  it('renders at the design size when nobody has touched it', () => {
    // A share link is opened by strangers. Enlarging their page without being
    // asked would be a surprise, so the default is the design size — the same
    // thing every other page they visit does.
    stubScreen(1440)
    expect(isShareFitEnabled()).toBe(false)
    expect(resolveShareUIScale()).toBe(1)

    applyShareUIScale()
    expect(document.documentElement.style.getPropertyValue(ROOT_ATTR)).toBe('1')
  })

  it('treats an unreadable stored value as off', () => {
    localStorage.setItem(SHARE_FIT_KEY, 'yes-please')
    expect(isShareFitEnabled()).toBe(false)
    expect(resolveShareUIScale()).toBe(1)
  })
})

describe('turning it on fits the page to the screen', () => {
  it('applies the screen-derived factor immediately', () => {
    stubScreen(1440) // → 1440/1080 = 1.33
    const factor = setShareFitEnabled(true)

    expect(factor).toBe(computeAutoUIScale(1440))
    expect(factor).toBeGreaterThan(1)
    expect(document.documentElement.style.getPropertyValue(ROOT_ATTR)).toBe(String(factor))
  })

  it('leaves 1080p and smaller at the design size', () => {
    // Phones are already scaled by the OS to their density and 1080p is the
    // design reference — fitting those would make the page BIGGER than the app
    // for no reason. `computeAutoUIScale` floors at 1, and the switch stays on
    // so a later, larger screen still fits.
    for (const edge of [768, 1080]) {
      stubScreen(edge)
      expect(setShareFitEnabled(true)).toBe(1)
      expect(document.documentElement.style.getPropertyValue(ROOT_ATTR)).toBe('1')
      expect(isShareFitEnabled()).toBe(true)
    }
  })

  it('also sets the integer input line box the form controls need', () => {
    // `applyUIScale` writes this alongside the factor (Android WebView rounds
    // the line box and the content box independently, so a fractional value
    // drifts the caret). Asserting it keeps the entry from dropping half of the
    // applier's contract.
    stubScreen(1440)
    setShareFitEnabled(true)
    expect(document.documentElement.style.getPropertyValue('--input-line-height')).toMatch(/^\d+px$/)
  })
})

describe('turning it back off restores the design size', () => {
  it('returns to 100% and forgets the choice', () => {
    stubScreen(1440)
    setShareFitEnabled(true)
    expect(document.documentElement.style.getPropertyValue(ROOT_ATTR)).not.toBe('1')

    const factor = setShareFitEnabled(false)
    expect(factor).toBe(1)
    expect(document.documentElement.style.getPropertyValue(ROOT_ATTR)).toBe('1')
    expect(isShareFitEnabled()).toBe(false)
  })

  it('does nothing visible on a screen that fits to 1 anyway', () => {
    // A 1080p reader can press it and see no change — that is correct, not a
    // broken button: the page already fits. The pressed state still flips, so
    // the control never looks dead.
    stubScreen(1080)
    setShareFitEnabled(true)
    const on = document.documentElement.style.getPropertyValue(ROOT_ATTR)
    setShareFitEnabled(false)
    const off = document.documentElement.style.getPropertyValue(ROOT_ATTR)
    expect(on).toBe(off)
  })
})

describe('the switch survives a reload', () => {
  it('is remembered, so the reader is not asked again', () => {
    stubScreen(1440)
    setShareFitEnabled(true)
    // A fresh read, as the next page load would do before mount.
    expect(isShareFitEnabled()).toBe(true)
    expect(resolveShareUIScale()).toBe(computeAutoUIScale(1440))
  })
})

describe('the narrow-layout threshold follows the applied scale', () => {
  it('multiplies the design threshold by the factor', () => {
    // The outline rail grows with the scale while the window does not, so a
    // fixed `(max-width: 899px)` would keep the rail on a window where it
    // squeezes the text.
    stubScreen(1440)
    setShareFitEnabled(true)

    const m = shareNarrowQuery().match(/^\(max-width: (\d+)px\)$/)
    expect(m, `unexpected query: ${shareNarrowQuery()}`).not.toBeNull()
    expect(Number(m![1])).toBe(Math.round(SHARE_NARROW_MAX_DESIGN_PX * 1.33))
    expect(Number(m![1])).toBeGreaterThan(SHARE_NARROW_MAX_DESIGN_PX)
  })

  it('is the bare design width when the switch is off', () => {
    stubScreen(1440)
    setShareFitEnabled(false)
    expect(shareNarrowQuery()).toBe(`(max-width: ${SHARE_NARROW_MAX_DESIGN_PX}px)`)
  })

  it('is re-evaluated when the switch changes, not just on mount', () => {
    // The threshold is part of the media query STRING, and a MediaQueryList
    // only fires when the window crosses ITS OWN threshold — which may never
    // happen after the threshold moves. So a cached list plus "compute once on
    // mount" leaves the layout frozen: the reader turns fitting on, the rail
    // should collapse into a drawer, and nothing happens until they resize.
    //
    // Checked as a source guard because the failure is a missing WIRING, not a
    // wrong computation (the e2e covers the resulting behaviour in a browser).
    for (const file of ['src/share/ShareView.vue', 'src/share/SessionShareView.vue']) {
      const src = readWebFile(file)
      expect(src, `${file} must use the shared query`).toContain('shareNarrowQuery()')
      expect(src, `${file} still has a hard-coded narrow query`).not.toMatch(
        /matchMedia\(\s*['"]\(max-width: 899px\)['"]/,
      )
      // A rebuild that re-creates the list (and re-reads the new threshold).
      expect(src, `${file} must rebuild the query on scale changes`).toMatch(
        /function rebuildNarrowQuery\(\)[\s\S]{0,400}matchMedia\(shareNarrowQuery\(\)\)/,
      )
      expect(src, `${file} must subscribe to the scale event`).toContain(
        "window.addEventListener(UI_SCALE_EVENT, rebuildNarrowQuery)",
      )
      expect(src, `${file} must unsubscribe on unmount`).toContain(
        "window.removeEventListener(UI_SCALE_EVENT, rebuildNarrowQuery)",
      )
      // Crossing the breakpoint resets the outline to that mode's default;
      // otherwise turning fitting ON pops a drawer over the page (covering the
      // switch), and turning it OFF leaves the outline hidden entirely.
      expect(src, `${file} must re-decide the outline state`).toContain(
        'if (isNarrow.value !== wasNarrow) tocOpen.value = !isNarrow.value',
      )
    }
  })
})

describe('the entry point applies it before mounting', () => {
  it('calls applyShareUIScale before createApp', () => {
    // Mounting first would paint one frame at the design size and then reflow
    // for a reader who left the switch on.
    const src = readWebFile('src/share/main.ts')
    const scaleAt = src.indexOf('applyShareUIScale()')
    const mountAt = src.indexOf('createApp(ShareRoot)')
    expect(scaleAt, 'share/main.ts must apply the scale').toBeGreaterThan(-1)
    expect(mountAt).toBeGreaterThan(-1)
    expect(scaleAt).toBeLessThan(mountAt)
  })
})

describe('the switch is reachable from every share topbar', () => {
  // Four pages carry the shared chrome: a file, a folder, a conversation and a
  // finished meeting record. A reader who lands on one of them must be able to
  // resize it — an unlisted page would be a dead end for exactly the people the
  // switch exists for.
  const pages = [
    'src/share/ShareView.vue',
    'src/share/DirShareView.vue',
    'src/share/SessionShareView.vue',
    'src/share/MeetingShareDetail.vue',
  ]

  it.each(pages)('%s renders the button in the topbar', (file) => {
    const src = readWebFile(file)
    expect(src, `${file} must import the button`).toContain(
      "import ShareFitButton from './ShareFitButton.vue'",
    )
    // Scoped to the topbar REGION, not to a single element: a non-greedy
    // `<div …>[\s\S]*?</div>` stops at the first nested close tag (the action
    // group), so it would miss a button placed after it.
    //
    // The region runs from the opening tag to the next top-level sibling, which
    // differs per page: the file/folder/conversation views open `.share-body`
    // and the finished-meeting record opens `.share-meeting__meta`. Anchoring
    // on either of them keeps the check about "inside the bar" rather than
    // about one page's layout.
    const start = src.indexOf('<div class="share-topbar">')
    expect(start, `${file} has no .share-topbar`).toBeGreaterThan(-1)
    const end = src
      .slice(start)
      .search(/<div class="(share-body|share-meeting__meta)"/)
    expect(end, `${file} has no element after the topbar to bound it`).toBeGreaterThan(0)
    expect(
      src.slice(start, start + end),
      `${file} must render the switch in the topbar`,
    ).toContain('<ShareFitButton />')
  })
})

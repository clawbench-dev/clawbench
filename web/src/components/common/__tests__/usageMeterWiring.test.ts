import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Source-level guards for the usage-meter UI.
 *
 * These are source assertions rather than mount tests on purpose: the two
 * invariants below are about module DEPENDENCIES and a wiring contract, which a
 * component test cannot observe (a mounted AppHeader would happily render while
 * quietly pulling ECharts into the first-paint bundle, and a capsule that never
 * reacts to the open request still looks fine until a notification is tapped).
 */

function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '')
}

const header = stripComments(readWebFile('src/components/common/AppHeader.vue'))
const popover = stripComments(readWebFile('src/components/common/UsageMeterPopover.vue'))

describe('usage meter — first-paint dependency guard', () => {
  it('AppHeader does not import ECharts (directly or via the stats chart module)', () => {
    // The stats panel lazy-loads ECharts; the header is on the critical path.
    // Importing statsChart from here would silently move the chart library into
    // the initial bundle — a regression no behavioural test can see.
    expect(header).not.toMatch(/from ['"]echarts/)
    expect(header).not.toMatch(/from ['"]@\/components\/stats\/statsChart/)
    expect(popover).not.toMatch(/from ['"]echarts/)
    expect(popover).not.toMatch(/from ['"]@\/components\/stats\/statsChart/)
  })

  it('the popover draws its curve with inline SVG rather than a chart library', () => {
    expect(popover).toContain('<svg')
    expect(popover).toContain('polyline')
  })
})

describe('usage meter — capsule wiring', () => {
  it('the capsule is gated on isWideScreen (no mobile header UI)', () => {
    // Mobile has no capsule by design: the meter is header-only and the phone
    // channel is the notification.
    expect(header).toMatch(/v-if="isWideScreen && usageMeter\.status\.value\.enabled"/)
  })

  it('the capsule renders before the tips ticker so the ticker absorbs slack', () => {
    const capsule = header.indexOf('<UsageMeterPopover')
    const ticker = header.indexOf('<ShortcutTipTicker')
    expect(capsule).toBeGreaterThan(-1)
    expect(ticker).toBeGreaterThan(-1)
    expect(capsule).toBeLessThan(ticker)
  })

  it('the popover reacts to the open request (notification tap)', () => {
    // A tap on a usage notification dispatches a window event; App.vue forwards
    // it to requestOpen(), and the popover must watch that counter. Without the
    // watch the tap opens nothing and the notification looks broken.
    expect(popover).toMatch(/watch\(\s*openRequest/)
  })

  it('App.vue forwards the notification tap to the meter', () => {
    const app = stripComments(readWebFile('src/App.vue'))
    expect(app).toContain("addEventListener('clawbench-open-usage-meter'")
    expect(app).toMatch(/function handleOpenUsageMeter/)
    // Narrow screens have no capsule, so the fallback must be the settings page —
    // and specifically the usage-meter category, which is its own settings entry
    // beside push notifications. Pointing at 'notification' would land the user
    // in the push-channel category, where the rule is not configured.
    expect(app).toMatch(/setPendingSettingsCategory\('usageMeter'\)/)
  })
})

describe('usage meter — notification channels', () => {
  it('the browser/system notification path handles usage_meter_update', () => {
    // Without this branch the event falls through to `else { return }` and
    // desktop/Electron never raise a system notification for a usage alert —
    // only Android and the IM channels would. The wording comes from the
    // payload, so the branch must read data.title / data.body.
    const src = stripComments(readWebFile('src/composables/useGlobalEvents.ts'))
    const at = src.indexOf("event === 'usage_meter_update'")
    expect(at).toBeGreaterThan(-1)

    const body = src.slice(at, at + 600)
    expect(body).toContain('data.title')
    expect(body).toContain('data.body')
    expect(body).toContain('clawbench-open-usage-meter')
  })

  it('the notification nav carries a usageMeter discriminator for the native shell', () => {
    // The Electron shell routes clicks by nav fields; without an explicit flag
    // it falls back to the session channel and drops the click.
    const src = stripComments(readWebFile('src/composables/useGlobalEvents.ts'))
    expect(src).toMatch(/usageMeter:\s*event === 'usage_meter_update'/)

    const native = stripComments(readWebFile('src/utils/clawbenchNative.ts'))
    expect(native).toMatch(/usageMeter\?:/)
  })
})

describe('usage meter capsule carries an icon and a word', () => {
  it('renders a gauge icon and a short label, not just numbers', () => {
    // A bar plus "12 / 20" does not say WHAT is measured. Sitting next to the
    // file and project badges it read as another file counter. The recent-project
    // rows solve this the same way: an icon followed by a word.
    const src = stripComments(readWebFile('src/components/common/UsageMeterPopover.vue'))
    expect(src).toMatch(/from 'lucide-vue-next'/)
    expect(src).toContain('<Gauge')
    expect(src).toContain('class="usage-label"')
    expect(src).toContain("t('usageMeter.label')")
  })

  it('both locales define the short label', () => {
    for (const loc of ['zh', 'en']) {
      const src = readWebFile(`src/i18n/locales/${loc}.ts`)
      expect(src).toMatch(/usageMeter:\s*\{[\s\S]*?\blabel:\s*'[^']+'/)
    }
  })
})

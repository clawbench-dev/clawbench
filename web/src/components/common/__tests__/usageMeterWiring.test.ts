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
    expect(header).toMatch(
      /const showUsageMeter = computed\(\(\) => isWideScreen\.value && usageMeter\.hasData\.value\)/,
    )
    expect(header).toMatch(/v-if="showUsageMeter"/)
    // And NOT on `enabled`: an unconfigured user still gets a spend total, so
    // gating on the rule would hide the capsule for exactly the people who
    // never opened the settings page.
    expect(header).not.toMatch(/v-if="isWideScreen && usageMeter\.status\.value\.enabled"/)
  })

  it('the capsule sits between the tips ticker and the theme button', () => {
    // Right-hand control cluster: ticker (flex:1) → capsule → theme → server.
    // Before the ticker the capsule was pushed to the middle of the header,
    // which read as a free-floating number rather than a header control.
    const capsule = header.indexOf('<UsageMeterPopover')
    const ticker = header.indexOf('<ShortcutTipTicker')
    const theme = header.indexOf('class="theme-quick-toggle"')
    expect(capsule).toBeGreaterThan(-1)
    expect(ticker).toBeGreaterThan(-1)
    expect(theme).toBeGreaterThan(-1)
    expect(ticker).toBeLessThan(capsule)
    expect(capsule).toBeLessThan(theme)
  })

  it('the capsule owns the push to the right edge, not the theme button', () => {
    // The capsule must be pinned right by its OWN `margin-left: auto`.
    //
    // This used to be the theme button's job, which only looked correct because
    // the shortcut-tips ticker (`flex: 1`) ate the slack and dragged the capsule
    // along with it. The ticker is hidden on tablets / the Android WebView
    // (`isAppMode && !isDesktopApp`), so there the capsule collapsed back beside
    // the project badge while the theme button flew to the right edge alone.
    // Asserting the auto margin lives on the FIRST element of the cluster is the
    // only source-level way to state "alignment must not depend on the ticker".
    const slot = header.slice(header.indexOf('.usage-meter-slot {'))
    expect(slot.slice(0, slot.indexOf('}'))).toContain('margin-left: auto')

    // The theme button keeps its auto margin as the fallback for when no capsule
    // is rendered at all (narrow screens, first fetch not landed yet)…
    const themeRule = header.slice(header.indexOf('.theme-quick-toggle {'))
    expect(themeRule.slice(0, themeRule.indexOf('}'))).toContain('margin-left: auto')
    // …but drops it while the capsule is present, or the two auto margins would
    // split the slack and strand the capsule in the middle.
    const hasMeter = header.slice(header.indexOf('.header--has-meter .theme-quick-toggle {'))
    expect(hasMeter.slice(0, hasMeter.indexOf('}'))).toContain('margin-left: 0')
  })

  it('the header--has-meter class and the capsule render gate cannot drift', () => {
    // Both must read the same expression: a class without the element leaves the
    // theme button unpushed (cluster drifts left), an element without the class
    // leaves the capsule stranded next to the project badge.
    expect(header).toMatch(/'header--has-meter': showUsageMeter/)
    expect(header).toMatch(/v-if="showUsageMeter"/)
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

describe('usage meter — mobile entry to the rule', () => {
  it('the phone row is the entry point to the settings category', () => {
    // The capsule's "adjust" link is desktop-only. On a phone the resources row
    // is the only usage surface, so it must also reach the rule — otherwise the
    // number is unexplainable and unchangeable there.
    expect(header).toContain('@open-usage-settings="handleOpenUsageSettings"')
    expect(header).toMatch(/function handleOpenUsageSettings/)
    const at = header.indexOf('function handleOpenUsageSettings')
    const body = header.slice(at, at + 400)
    expect(body).toContain("setPendingSettingsCategory('usageMeter')")
    expect(body).toContain("switchTab?.('settings')")
    // The popover is teleported to <body>; leaving it open would float it over
    // the settings tab it just navigated to.
    expect(body).toContain('resourcesMenuOpen.value = false')

    const panel = stripComments(readWebFile('src/components/common/SystemResourcesPanel.vue'))
    expect(panel).toContain("$emit('open-usage-settings')")
    expect(panel).toContain("defineEmits(['switch-server', 'open-usage-settings'])")
  })
})

describe('usage meter — live number updates', () => {
  it('subscribes to the tick event, not only the threshold event', () => {
    // usage_meter_update means "a threshold was crossed" — every notification
    // surface fires for it. Using it for plain data changes would pop a system
    // notification after every completed turn. The capsule therefore needs its
    // own event, or it stays stale until the 5-minute worker or a manual open.
    const src = stripComments(readWebFile('src/composables/useUsageMeter.ts'))
    expect(src).toContain("'usage_meter_tick'")
    expect(src).toContain("'usage_meter_update'")
  })

  it('the backend broadcasts on change and skips an unchanged reading', () => {
    const src = stripComments(readWebFile('../internal/service/usage_meter.go'))
    expect(src).toContain('UsageMeterTickEventType = "usage_meter_tick"')
    expect(src).toMatch(/func broadcastUsageMeterNumbers\(/)
    // Called from the check path, so a completed turn reaches it.
    expect(src).toContain('broadcastUsageMeterNumbers(st)')
  })
})

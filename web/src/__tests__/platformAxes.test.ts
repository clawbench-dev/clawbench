import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the three platform axes are independent, and consumers name the one
 * they mean.
 *
 * `isPC` used to answer "what kind of machine is this?" with a single boolean
 * that OR'd together three unrelated questions:
 *
 *   HOST     — which shell hosts the app (Electron / Android WebView / browser)
 *   INPUT    — the primary pointer (touch vs mouse + keyboard)
 *   VIEWPORT — how much room there is (isWideScreen)
 *
 * Every edge case fell out of that collapse: an Electron window resized narrow
 * (host=desktop, viewport=compact), a touchscreen laptop (host=browser,
 * input=touch), an Android tablet in landscape (host=Android, viewport=wide).
 * The axes are now separate, so the regression to catch is a consumer reaching
 * for a conflated predicate again — or a copy of one of the shared host
 * predicates being re-declared locally, which is how the four byte-identical
 * `isAndroidApp` copies drifted apart in the first place.
 */

/** Strip comments so prose naming a predicate cannot satisfy a check. */
function stripComments(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '')
}

describe('platform axes stay orthogonal', () => {
  it('the conflated isPC predicate is gone from source', () => {
    // Named `isPC`, `_setIsPCForTest` — either would resurrect the collapse.
    const files = [
      'src/composables/usePlatformDetect.ts',
      'src/composables/useWideScreenLayout.ts',
      'src/composables/useDesktopDownload.ts',
      'src/components/file/FileManagerContent.vue',
      'src/components/terminal/TerminalPanelContent.vue',
      'src/components/chat/ChatInputBar.vue',
      'src/components/common/CompletionPopover.vue',
      'src/components/file/MarkdownPreview.vue',
      'src/components/settings/SettingsGroupPanel.vue',
      'src/components/settings/SettingsCategory.vue',
      'src/components/proxy/ProxyPanelContent.vue',
      'src/composables/useTableRowExpand.ts',
      'src/composables/useCodeLinkPreview.ts',
      'src/composables/useDoubleClickCopy.ts',
      'src/composables/useMarkdownRenderer.ts',
      'src/composables/useMarkdownRenderPipeline.ts',
      'src/utils/previewMarkdown.ts',
      'src/utils/chatRenderUtils.ts',
      'src/utils/terminalBlurUtils.ts',
    ] as const
    for (const rel of files) {
      const clean = stripComments(readWebFile(rel))
      expect(clean, `${rel}: isPC must not come back`).not.toMatch(/\bisPC\b/)
      expect(clean, `${rel}: _setIsPCForTest must not come back`).not.toContain('_setIsPCForTest')
    }
  })

  it('usePlatformDetect exports the three host axes plus the input axis', () => {
    const src = readWebFile('src/composables/usePlatformDetect.ts')
    for (const axis of ['isElectron', 'isAndroidApp', 'isWebApp', 'isNativeApp', 'isTouchPrimary']) {
      expect(src, `axis ${axis} must be exposed`).toContain(axis)
    }
    // The viewport axis lives in its own module — the module must not pretend
    // to own it (that is what made the old predicate a lie).
    expect(stripComments(src)).not.toContain('isWideScreen')
  })

  it('the shared host predicates are not re-declared in components', () => {
    // Four byte-identical `isAndroidApp = computed(...)` copies used to exist,
    // each with a comment claiming to match the others. Re-declaring one here
    // is the drift this prevents.
    const consumers = [
      'src/components/settings/SettingsGroupPanel.vue',
      'src/components/settings/SettingsCategory.vue',
      'src/components/file/FileManagerContent.vue',
      'src/components/proxy/ProxyPanelContent.vue',
    ] as const
    for (const rel of consumers) {
      const clean = stripComments(readWebFile(rel))
      expect(clean, `${rel}: must import isAndroidApp, not redeclare it`).not.toMatch(
        /isAndroidApp\s*=\s*computed\(/,
      )
      expect(clean, `${rel}: must not redeclare isDesktopShell`).not.toMatch(
        /isDesktopShell\s*=\s*computed\(/,
      )
      expect(clean, `${rel}: must not redeclare isWebMode`).not.toMatch(/isWebMode\s*=\s*computed\(/)
      // It must actually consume the shared module.
      expect(clean, `${rel}: must import usePlatformDetect`).toContain('usePlatformDetect')
    }
  })

  it('the iPadOS desktop-download trap stays closed', () => {
    // iPadOS 13+ sends a "Macintosh" UA, so a predicate testing only
    // Android/iOS offered an iPad the macOS build. `isMobileOSUA` exists to
    // cover it, and both the download flag and the platform key must use it.
    const src = readWebFile('src/composables/useDesktopDownload.ts')
    expect(src, 'isMobileOSUA must exist').toContain('isMobileOSUA')
    expect(src, 'the desktop flag must exclude mobile OSes').toMatch(
      /isDesktop\s*=\s*isWebApp\.value\s*&&\s*!isMobileOSUA/,
    )
    expect(src, 'detectPlatformKey must bail out for mobile OSes').toMatch(
      /detectPlatformKey[\s\S]*?if\s*\(isMobileOSUA\)\s*return\s*''/,
    )
  })
})

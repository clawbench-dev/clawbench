import { describe, expect, it } from 'vitest'

/**
 * Guards for the spinner unification.
 *
 * Six ad-hoc rotating-icon spinners were replaced by the shared
 * LoadingIndicator. Two things can silently regress:
 *
 *   1. The site keeps its own `animation:` (or `@keyframes`), which fights the
 *      component's own rotation and reintroduces a second implementation.
 *   2. The site loses its `--li-color`, so the spinner silently falls back to
 *      the global accent and stops matching the control it lives in.
 *
 * Both are invisible to behavioural tests (the element still spins either way),
 * so they are pinned at the source level. Each entry records the colour the
 * original icon carried, which is what must be preserved.
 */

/** Read an SFC / stylesheet as text. */
async function source(relPath: string): Promise<string> {
  const mod = await import(/* @vite-ignore */ `${relPath}?raw`)
  return typeof mod.default === 'string' ? mod.default : ''
}

/**
 * Every migrated site: the file, the tint class that carries its colour, and the
 * colour expression that class must keep.
 */
const MIGRATED = [
  {
    file: '@/components/settings/ForgeCredentialsRow.vue',
    cls: 'forge-spin',
    color: '--li-color: currentColor',
    why: 'verify button is a neutral .fbtn pill (grey text)',
  },
  {
    file: '@/components/common/DialogOverlay.vue',
    cls: 'dlg-generate-spin',
    color: '--li-color: currentColor',
    why: 'generate button is accent-tinted',
  },
  {
    file: '@/components/common/SystemResourcesPanel.vue',
    cls: 'status-reconnecting',
    color: '--li-color: var(--color-yellow, #eab308)',
    why: 'reconnecting warning is yellow',
  },
  {
    file: '@/components/file/SharedFilesDrawer.vue',
    cls: 'shared-files-clear-spin',
    color: '--li-color: currentColor',
    why: 'clear button is destructive red',
  },
  {
    file: '@/components/session/SharedSessionsDrawer.vue',
    cls: 'shared-sessions-clear-spin',
    color: '--li-color: currentColor',
    why: 'clear button is destructive red',
  },
  {
    file: '@/assets/share-dialog.css',
    cls: 'share-dialog-spin',
    color: '--li-color: currentColor',
    why: 'regenerate button uses --text-secondary',
  },
]

describe('spinner unification — migrated sites', () => {
  for (const { file, cls, color, why } of MIGRATED) {
    describe(`${cls} (${file})`, () => {
      it('keeps the colour the original icon carried', async () => {
        const src = await source(file)
        const rule = src.match(new RegExp(`\\.${cls}\\s*\\{([\\s\\S]*?)\\}`))
        expect(rule, `.${cls} rule must exist`).not.toBeNull()
        expect(
          rule![1],
          `${cls} must keep its tint (${why}) — otherwise the spinner falls back to the global accent`,
        ).toContain(color)
      })

      it('no longer animates itself (the component owns the rotation)', async () => {
        const src = await source(file)
        const rule = src.match(new RegExp(`\\.${cls}\\s*\\{([\\s\\S]*?)\\}`))
        expect(rule, `.${cls} rule must exist`).not.toBeNull()
        expect(
          rule![1],
          `${cls} must not declare its own animation — two rotating layers fight`,
        ).not.toContain('animation:')
        expect(
          src,
          `the local @keyframes for ${cls} must be gone`,
        ).not.toContain(`@keyframes ${cls}`)
      })
    })
  }

  it('renders the shared LoadingIndicator at every migrated site', async () => {
    const files = [
      '@/components/settings/ForgeCredentialsRow.vue',
      '@/components/common/DialogOverlay.vue',
      '@/components/common/SystemResourcesPanel.vue',
      '@/components/file/SharedFilesDrawer.vue',
      '@/components/session/SharedSessionsDrawer.vue',
      '@/components/session/SessionShareDialog.vue',
      '@/components/file/ShareLinkDialog.vue',
    ]
    for (const f of files) {
      const src = await source(f)
      expect(src, `${f} must render <LoadingIndicator>`).toContain('<LoadingIndicator')
      expect(src, `${f} must import LoadingIndicator`).toContain(
        "import LoadingIndicator from '@/components/common/LoadingIndicator.vue'",
      )
    }
  })

  it('uses the shared component\'s size tiers, not numeric sizes', async () => {
    // `size` is a 'sm' | 'md' | 'lg' enum: a number compiles to a class that
    // matches no rule and silently falls back to the 28px default.
    const files = [
      '@/components/settings/ForgeCredentialsRow.vue',
      '@/components/common/DialogOverlay.vue',
      '@/components/common/SystemResourcesPanel.vue',
      '@/components/file/SharedFilesDrawer.vue',
      '@/components/session/SharedSessionsDrawer.vue',
      '@/components/session/SessionShareDialog.vue',
      '@/components/file/ShareLinkDialog.vue',
    ]
    for (const f of files) {
      const src = await source(f)
      const tags = src.match(/<LoadingIndicator[^>]*>/g) || []
      expect(tags.length, `${f} must render at least one LoadingIndicator`).toBeGreaterThan(0)
      for (const tag of tags) {
        expect(tag, `${f}: ${tag} must use a size tier`).toMatch(/size="(sm|md|lg)"/)
        expect(tag, `${f}: ${tag} must not pass a numeric size`).not.toMatch(/:size="\d/)
      }
    }
  })
})

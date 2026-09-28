import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Slider reset buttons must stay in the layout at their default value.
 *
 * The defect this pins: the button was rendered with
 * `v-if="modelValue !== defaultValue"`, so it appeared only once a value had
 * been changed and vanished the moment the user reset it. Its removal reflowed
 * the row — the slider and the numeric value label jumped sideways on every
 * reset, which is the "layout shift" the user reported.
 *
 * The fix is to keep the button mounted and mark it `:disabled` at the default,
 * dimming it with the "unusable" opacity token. So the guards are:
 *
 *   1. The button's presence must not depend on the current value — that is the
 *      regression. A `v-if` gated on `defaultValue !== undefined` is fine (a row
 *      with nothing to reset to genuinely has no button); a `v-if` mentioning
 *      the current value is not.
 *   2. The `:disabled` binding must exist, or the inert button would still be
 *      clickable and would emit a no-op update on every press.
 *   3. The `:disabled` CSS rule must exist, or the button reads as active
 *      (full-strength `--text-muted`) while doing nothing.
 *
 * Two components own a copy of this button: the shared SettingsItem row
 * (uiScale / terminal font size / TTS speed) and WallpaperSetting's hand-rolled
 * rows (wave speed, per-style params, panel opacity, blur). They duplicate the
 * `.settings-item__slider-reset` class, so both are asserted.
 */
describe('slider reset button stays resident', () => {
  const SETTINGS_ITEM = 'src/components/settings/SettingsItem.vue'
  const WALLPAPER = 'src/components/settings/WallpaperSetting.vue'

  /** Expressions that read the value being edited, i.e. the ones that must NOT
   *  gate the button's presence. */
  const CURRENT_VALUE_REFS = ['modelValue', 'waveSpeed', 'panelOpacity', 'wallpaperBlur', 'styleParamValue']

  function resetButtonTags(src: string): string[] {
    // Match each `<button ... class="settings-item__slider-reset" ...>` opening
    // tag, across the line breaks some of them span.
    return src.match(/<button[^>]*settings-item__slider-reset[^>]*>/g) ?? []
  }

  it.each([SETTINGS_ITEM, WALLPAPER])('%s keeps the reset button independent of the current value', (file) => {
    const tags = resetButtonTags(readWebFile(file))
    expect(tags.length).toBeGreaterThan(0)
    for (const tag of tags) {
      const condition = tag.match(/v-if="([^"]*)"/)?.[1]
      if (condition === undefined) continue
      for (const ref of CURRENT_VALUE_REFS) {
        expect(condition).not.toContain(ref)
      }
    }
  })

  it.each([SETTINGS_ITEM, WALLPAPER])('%s disables the reset button at its default', (file) => {
    const tags = resetButtonTags(readWebFile(file))
    expect(tags.length).toBeGreaterThan(0)
    for (const tag of tags) {
      expect(tag).toMatch(/:disabled="[^"]+"/)
    }
  })

  it.each([SETTINGS_ITEM, WALLPAPER])('%s dims the disabled reset button', (file) => {
    const src = readWebFile(file)
    // Anchor on the newline so the rule body is matched, not the many other
    // selectors that merely start with the same prefix.
    const rule = src.match(/\n\.settings-item__slider-reset:disabled\s*\{([^}]*)\}/)
    if (!rule) throw new Error('missing .settings-item__slider-reset:disabled rule')
    expect(rule[1]).toContain('--opacity-disabled')
  })

  it('keeps the reset button when there is no default to reset to', () => {
    // Nothing to reset to means the control is genuinely absent, not inert —
    // a permanently dead button would be worse than no button.
    const src = readWebFile(SETTINGS_ITEM)
    expect(src).toMatch(/v-if="defaultValue !== undefined"/)
  })
})

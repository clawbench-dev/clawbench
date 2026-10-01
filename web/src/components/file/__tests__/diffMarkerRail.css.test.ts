import { describe, expect, it } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'

/**
 * Diff marker rail contract.
 *
 * The markdown preview's change marker used to be a 20px-tall solid block with
 * hard-coded orange/red/green (rgba(255,165,0,.7) etc). That broke two rules:
 *   1. Colour must come from theme tokens — every other diff surface in the app
 *      draws from --diff-{add,del,mod}-{bg,accent} (see css/diff-rows.css).
 *   2. Red/green alone cannot encode add-vs-delete (colour-vision deficiency);
 *      the repo's diff language pairs the tint with a 3px shape rail.
 *
 * The marker is now a 3px rail in --diff-*-accent; the M/D/+ label only appears
 * as a chip on hover/focus. These tests are source-contract checks (jsdom has
 * no CSS engine for var() resolution).
 */
describe('diff marker rail', () => {
  const markerCss = readFileSync(resolve(__dirname, '../../../assets/diff-marker.css'), 'utf8')
  const viewerCss = readFileSync(resolve(__dirname, '../../../assets/code-viewer.css'), 'utf8')

  it('draws the rail from theme tokens, not hard-coded rgba colours', () => {
    expect(markerCss).not.toMatch(/rgba\(\s*255\s*,\s*165\s*,\s*0/)
    expect(markerCss).not.toMatch(/rgba\(\s*255\s*,\s*80\s*,\s*80/)
    expect(markerCss).not.toMatch(/rgba\(\s*80\s*,\s*200\s*,\s*80/)
    expect(markerCss).toContain('var(--diff-mod-accent)')
    expect(markerCss).toContain('var(--diff-del-accent)')
    expect(markerCss).toContain('var(--diff-add-accent)')
  })

  it('draws the rail 3px wide', () => {
    const rail = markerCss.match(/\.diff-marker::before\s*\{[\s\S]*?\}/)
    expect(rail).toBeTruthy()
    expect(rail![0]).toContain('width: 3px')
  })

  it('keeps a tappable hit area wider than the rail', () => {
    // The button is the hit area; the rail is drawn at its right edge. A 3px
    // hit target would be unhittable, so the element stays 20px.
    const rule = viewerCss.match(/\.diff-marker-inline\s*\{[\s\S]*?\}/)
    expect(rule).toBeTruthy()
    expect(rule![0]).toContain('width: 20px')
  })

  it('has no entry keyframes (a rail appearing is not an event to animate)', () => {
    expect(markerCss).not.toContain('@keyframes')
  })
})

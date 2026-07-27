import { describe, it, expect } from 'vitest'

describe('SystemResourcesPanel', () => {
  it('should be importable', async () => {
    const mod = await import('../../common/SystemResourcesPanel.vue')
    expect(mod.default).toBeDefined()
  })
})

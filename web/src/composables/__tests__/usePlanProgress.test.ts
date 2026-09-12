import { describe, expect, it, beforeEach } from 'vitest'
import { usePlanProgress, clearPlanState, updatePlanEntries, setPlanCollapsed } from '@/composables/usePlanProgress'

describe('usePlanProgress', () => {
  beforeEach(() => {
    clearPlanState()
  })

  it('hasPlan is false when no entries', () => {
    const { hasPlan } = usePlanProgress()
    expect(hasPlan.value).toBe(false)
  })

  it('hasPlan is true after updatePlanEntries', () => {
    const { hasPlan } = usePlanProgress()
    updatePlanEntries([{ content: 'Step 1', priority: 'high', status: 'pending' }])
    expect(hasPlan.value).toBe(true)
  })

  it('replaces entries on update (ACP replace semantics)', () => {
    const { planEntries } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    updatePlanEntries([{ content: 'B', priority: 'medium', status: 'in_progress' }])
    expect(planEntries.value).toHaveLength(1)
    expect(planEntries.value[0].content).toBe('B')
  })

  it('inProgressEntry returns first in_progress entry', () => {
    const { inProgressEntry } = usePlanProgress()
    updatePlanEntries([
      { content: 'Done', priority: 'high', status: 'completed' },
      { content: 'Working', priority: 'medium', status: 'in_progress' },
      { content: 'Later', priority: 'low', status: 'pending' },
    ])
    expect(inProgressEntry.value).toBeDefined()
    expect(inProgressEntry.value!.content).toBe('Working')
  })

  it('inProgressEntry is undefined when nothing in progress', () => {
    const { inProgressEntry } = usePlanProgress()
    updatePlanEntries([
      { content: 'Done', priority: 'high', status: 'completed' },
      { content: 'Later', priority: 'low', status: 'pending' },
    ])
    expect(inProgressEntry.value).toBeUndefined()
  })

  it('completedCount and totalCount work correctly', () => {
    const { completedCount, totalCount } = usePlanProgress()
    updatePlanEntries([
      { content: 'A', priority: 'high', status: 'completed' },
      { content: 'B', priority: 'medium', status: 'in_progress' },
      { content: 'C', priority: 'low', status: 'pending' },
    ])
    expect(completedCount.value).toBe(1)
    expect(totalCount.value).toBe(3)
  })

  it('clearPlanState resets everything', () => {
    const { hasPlan, planCollapsed, planHasUpdate } = usePlanProgress()
    updatePlanEntries([{ content: 'X', priority: 'high', status: 'pending' }])
    setPlanCollapsed(false)
    expect(hasPlan.value).toBe(true)
    clearPlanState()
    expect(hasPlan.value).toBe(false)
    expect(planCollapsed.value).toBe(true)
    expect(planHasUpdate.value).toBe(false)
  })

  it('defaults to collapsed and shows no update glow when a plan first appears', () => {
    const { planCollapsed, planHasUpdate } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    expect(planCollapsed.value).toBe(true)
    expect(planHasUpdate.value).toBe(false)
  })

  it('opens collapsed again after being cleared', () => {
    const { planCollapsed } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    setPlanCollapsed(false)
    clearPlanState()
    updatePlanEntries([{ content: 'B', priority: 'high', status: 'pending' }])
    expect(planCollapsed.value).toBe(true)
  })

  it('preserves the expanded choice across updates to a visible plan', () => {
    const { planCollapsed } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    setPlanCollapsed(false)
    updatePlanEntries([{ content: 'B', priority: 'high', status: 'in_progress' }])
    expect(planCollapsed.value).toBe(false)
  })

  it('planHasUpdate is set when a collapsed plan updates', () => {
    const { planCollapsed, planHasUpdate } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    expect(planCollapsed.value).toBe(true)
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'in_progress' }])
    expect(planHasUpdate.value).toBe(true)
  })

  it('planHasUpdate is not set when expanded and entries update', () => {
    const { planHasUpdate } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    setPlanCollapsed(false)
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'in_progress' }])
    expect(planHasUpdate.value).toBe(false)
  })

  it('expanding clears planHasUpdate', () => {
    const { planHasUpdate } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'in_progress' }])
    expect(planHasUpdate.value).toBe(true)
    setPlanCollapsed(false)
    expect(planHasUpdate.value).toBe(false)
  })

  it('togglePlanCollapse flips state and clears update on expand', () => {
    const { planCollapsed, planHasUpdate, togglePlanCollapse } = usePlanProgress()
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'pending' }])
    updatePlanEntries([{ content: 'A', priority: 'high', status: 'in_progress' }])
    expect(planHasUpdate.value).toBe(true)
    togglePlanCollapse()
    expect(planCollapsed.value).toBe(false)
    expect(planHasUpdate.value).toBe(false)
  })
})

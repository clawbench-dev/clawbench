import { describe, expect, it, vi, beforeEach } from 'vitest'

// Use vi.hoisted to define mocks before vi.mock factory runs
const { mockOpenDiffDrawer, mockDiffMarkers } = vi.hoisted(() => {
  const mockOpenDiffDrawer = vi.fn()
  // Use a plain object with a .value property (mimicking Vue ref)
  const mockDiffMarkers = { value: [] }
  return { mockOpenDiffDrawer, mockDiffMarkers }
})

vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  diffMarkers: mockDiffMarkers,
  openDiffDrawer: (...args: unknown[]) => mockOpenDiffDrawer(...args),
}))

// Import after mocks
import { handleDiffMarkerClick } from '@/composables/useDiffMarkerClick'

describe('handleDiffMarkerClick', () => {
  beforeEach(() => {
    mockOpenDiffDrawer.mockReset()
    mockDiffMarkers.value = []
  })

  it('returns false when no marker element is clicked', () => {
    const event = new Event('click')
    Object.defineProperty(event, 'target', { value: document.createElement('div') })

    const result = handleDiffMarkerClick(event, '.diff-marker-inline')

    expect(result).toBe(false)
    expect(mockOpenDiffDrawer).not.toHaveBeenCalled()
  })

  it('returns true and opens drawer when a marker is clicked', () => {
    const marker = { id: 'm1', type: 'modified', label: 'M' }
    mockDiffMarkers.value = [marker]

    const markerEl = document.createElement('div')
    markerEl.className = 'diff-marker-inline'
    markerEl.setAttribute('data-marker-id', 'm1')

    const event = new Event('click', { cancelable: true })
    const targetEl = document.createElement('span')
    targetEl.closest = vi.fn().mockReturnValue(markerEl)
    Object.defineProperty(event, 'target', { value: targetEl })

    const result = handleDiffMarkerClick(event, '.diff-marker-inline')

    expect(result).toBe(true)
    expect(event.defaultPrevented).toBe(true)
    expect(mockOpenDiffDrawer).toHaveBeenCalledWith(marker)
  })

  it('prevents default and stops propagation on marker click', () => {
    const marker = { id: 'm2', type: 'deleted', label: 'D' }
    mockDiffMarkers.value = [marker]

    const markerEl = document.createElement('div')
    markerEl.className = 'diff-marker-overlay'
    markerEl.setAttribute('data-marker-id', 'm2')

    const event = new Event('click', { cancelable: true })
    const targetEl = document.createElement('span')
    targetEl.closest = vi.fn().mockReturnValue(markerEl)
    Object.defineProperty(event, 'target', { value: targetEl })

    handleDiffMarkerClick(event, '.diff-marker-overlay')

    expect(event.defaultPrevented).toBe(true)
  })

  it('does not open drawer when marker ID is not found in diffMarkers', () => {
    mockDiffMarkers.value = [{ id: 'other', type: 'modified', label: 'M' }]

    const markerEl = document.createElement('div')
    markerEl.className = 'diff-marker-inline'
    markerEl.setAttribute('data-marker-id', 'nonexistent')

    const event = new Event('click', { cancelable: true })
    const targetEl = document.createElement('span')
    targetEl.closest = vi.fn().mockReturnValue(markerEl)
    Object.defineProperty(event, 'target', { value: targetEl })

    const result = handleDiffMarkerClick(event, '.diff-marker-inline')

    expect(result).toBe(true)
    expect(mockOpenDiffDrawer).not.toHaveBeenCalled()
  })

  it('does not open drawer when data-marker-id attribute is missing', () => {
    const markerEl = document.createElement('div')
    markerEl.className = 'diff-marker-inline'

    const event = new Event('click', { cancelable: true })
    const targetEl = document.createElement('span')
    targetEl.closest = vi.fn().mockReturnValue(markerEl)
    Object.defineProperty(event, 'target', { value: targetEl })

    const result = handleDiffMarkerClick(event, '.diff-marker-inline')

    expect(result).toBe(true)
    expect(mockOpenDiffDrawer).not.toHaveBeenCalled()
  })

  it('works with overlay selector', () => {
    const marker = { id: 'm3', type: 'added', label: '+' }
    mockDiffMarkers.value = [marker]

    const markerEl = document.createElement('div')
    markerEl.className = 'diff-marker-overlay'
    markerEl.setAttribute('data-marker-id', 'm3')

    const event = new Event('click', { cancelable: true })
    const targetEl = document.createElement('span')
    targetEl.closest = vi.fn().mockReturnValue(markerEl)
    Object.defineProperty(event, 'target', { value: targetEl })

    const result = handleDiffMarkerClick(event, '.diff-marker-overlay')

    expect(result).toBe(true)
    expect(mockOpenDiffDrawer).toHaveBeenCalledWith(marker)
  })
})

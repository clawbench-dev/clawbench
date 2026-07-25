import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock agentIcons to provide predictable SVG data
vi.mock('@/utils/agentIcons', () => ({
  getAgentSvg: (id: string) => {
    const map: Record<string, { svg: string; viewBox: string; needsBg?: boolean }> = {
      codebuddy: {
        svg: '<defs><radialGradient id="ai-cb-g"><stop stop-color="#2EA99D"/></radialGradient></defs><path fill="url(#ai-cb-g)" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
      },
      claude: {
        svg: '<path fill="#D97757" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
      },
      opencode: {
        svg: '<path fill="#4A4A4A" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
        needsBg: true,
      },
    }
    return map[id] ?? null
  },
}))

import AgentIcon from '@/components/common/AgentIcon.vue'

function mountIcon(props = {}) {
  return mount(AgentIcon, {
    props: {
      backend: 'codebuddy',
      name: 'CodeBuddy',
      size: 16,
      ...props,
    },
  })
}

describe('AgentIcon', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('SVG rendering', () => {
    it('renders SVG when backend has a logo', () => {
      const wrapper = mountIcon({ backend: 'codebuddy' })
      expect(wrapper.find('svg').exists()).toBe(true)
    })

    it('renders initial letter when backend has no logo', () => {
      const wrapper = mountIcon({ backend: 'unknown', name: undefined })
      expect(wrapper.find('svg').exists()).toBe(false)
      expect(wrapper.find('.agent-icon-initial').exists()).toBe(true)
      expect(wrapper.text()).toBe('U') // first letter of 'unknown'
    })

    it('uses name initial when name is provided for unknown backend', () => {
      const wrapper = mountIcon({ backend: 'unknown', name: 'MyAgent' })
      expect(wrapper.text()).toBe('M')
    })
  })

  describe('SVG gradient ID uniqueness', () => {
    it('adds unique suffix to id and url(#...) references', () => {
      const wrapper = mountIcon({ backend: 'codebuddy' })
      const svgHtml = wrapper.find('svg').html()

      // Should contain id="ai-cb-g_" with a suffix
      expect(svgHtml).toMatch(/id="ai-cb-g_[a-z0-9]+"/)
      // Should contain url(#ai-cb-g_) with the same suffix
      expect(svgHtml).toMatch(/url\(#ai-cb-g_[a-z0-9]+\)/)
    })

    it('generates different suffixes for different instances', () => {
      const wrapper1 = mountIcon({ backend: 'codebuddy' })
      const wrapper2 = mountIcon({ backend: 'codebuddy' })

      const html1 = wrapper1.find('svg').html()
      const html2 = wrapper2.find('svg').html()

      // Extract the suffix from each
      const match1 = html1.match(/id="ai-cb-g_([a-z0-9]+)"/)
      const match2 = html2.match(/id="ai-cb-g_([a-z0-9]+)"/)

      expect(match1).toBeTruthy()
      expect(match2).toBeTruthy()
      expect(match1![1]).not.toBe(match2![1])
    })

    it('does not modify SVGs without ai- prefixed IDs', () => {
      const wrapper = mountIcon({ backend: 'claude' })
      const svgHtml = wrapper.find('svg').html()

      // claude SVG has no ai- prefixed IDs, should remain unchanged
      expect(svgHtml).toContain('fill="#D97757"')
      expect(svgHtml).not.toMatch(/id="ai-/)
    })
  })

  describe('size prop', () => {
    it('applies width and height from size prop', () => {
      const wrapper = mountIcon({ size: 32 })
      const svg = wrapper.find('svg')
      expect(svg.attributes('style')).toContain('width: 32px')
      expect(svg.attributes('style')).toContain('height: 32px')
    })

    it('uses default size of 16', () => {
      const wrapper = mountIcon()
      const svg = wrapper.find('svg')
      expect(svg.attributes('style')).toContain('width: 16px')
    })
  })

  describe('background for low-contrast icons', () => {
    it('adds bg class when needsBg is true', () => {
      const wrapper = mountIcon({ backend: 'opencode' })
      const svg = wrapper.find('svg')
      expect(svg.classes()).toContain('agent-icon-bg')
    })

    it('does not add bg class when needsBg is false', () => {
      const wrapper = mountIcon({ backend: 'codebuddy' })
      const svg = wrapper.find('svg')
      expect(svg.classes()).not.toContain('agent-icon-bg')
    })
  })

  describe('accessibility', () => {
    it('adds role="img" to SVG', () => {
      const wrapper = mountIcon({ backend: 'codebuddy' })
      const svg = wrapper.find('svg')
      expect(svg.attributes('role')).toBe('img')
    })

    it('uses name for aria-label when provided', () => {
      const wrapper = mountIcon({ backend: 'codebuddy', name: 'CodeBuddy' })
      const svg = wrapper.find('svg')
      expect(svg.attributes('aria-label')).toBe('CodeBuddy')
    })

    it('falls back to backend for aria-label when name is not provided', () => {
      const wrapper = mountIcon({ backend: 'claude', name: undefined })
      const svg = wrapper.find('svg')
      expect(svg.attributes('aria-label')).toBe('claude')
    })
  })
})

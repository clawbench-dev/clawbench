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
    const map: Record<string, { svg: string; viewBox: string; needsBg?: boolean; monoCssClass?: string }> = {
      codebuddy: {
        svg: '<defs><radialGradient id="ai-cb-g"><stop stop-color="#2EA99D"/></radialGradient></defs><path fill="url(#ai-cb-g)" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
      },
      claude: {
        svg: '<path fill="#D97757" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
      },
      lobeicons: {
        svg: '<defs><radialGradient id="lobe-icons-grad-0"><stop stop-color="#2EA99D"/></radialGradient></defs><path fill="url(#lobe-icons-grad-0)" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
      },
      opencode: {
        svg: '<path fill="currentColor" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
        needsBg: true,
        monoCssClass: 'mono-opencode',
      },
      pi: {
        svg: '<path fill="currentColor" d="M0 0h24v24H0z"/>',
        viewBox: '0 0 24 24',
        needsBg: true,
        monoCssClass: 'mono-pi',
      },
      noBgColor: {
        svg: '<path fill="#333" d="M0 0h24v24H0z"/>',
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

  describe('custom avatar', () => {
    it('renders an <img> with an encoded data URI when avatar is set', () => {
      const svg = '<svg viewBox="0 0 2 2"><rect width="2" height="2" fill="#123"/></svg>'
      const wrapper = mountIcon({ avatar: svg })
      const img = wrapper.find('img.agent-icon-img')
      expect(img.exists()).toBe(true)
      // The SVG must be URL-encoded so `#` in fill/url(#id) does not truncate.
      expect(img.attributes('src')).toBe('data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg))
      expect(img.attributes('src')).toContain('%23') // '#' encoded
      // Custom avatar takes precedence over the backend SVG.
      expect(wrapper.find('svg').exists()).toBe(false)
    })

    it('falls back to the backend icon when avatar is empty', () => {
      const wrapper = mountIcon({ backend: 'codebuddy', avatar: '' })
      expect(wrapper.find('img.agent-icon-img').exists()).toBe(false)
      expect(wrapper.find('svg').exists()).toBe(true)
    })

    it('applies the size prop to the avatar image', () => {
      const wrapper = mountIcon({ avatar: '<svg/>', size: 24 })
      const img = wrapper.find('img.agent-icon-img')
      expect(img.attributes('style')).toContain('width: 24px')
      expect(img.attributes('style')).toContain('height: 24px')
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

    it('adds unique suffix to lobe-icons- prefixed id and url(#...) references', () => {
      const wrapper = mountIcon({ backend: 'lobeicons' })
      const svgHtml = wrapper.find('svg').html()

      // Should contain id="lobe-icons-grad-0_" with a suffix
      expect(svgHtml).toMatch(/id="lobe-icons-grad-0_[a-z0-9]+"/)
      // Should contain url(#lobe-icons-grad-0_) with the same suffix —
      // url(#...) references must use same suffix as defs id= to keep gradients working
      expect(svgHtml).toMatch(/url\(#lobe-icons-grad-0_[a-z0-9]+\)/)

      // Verify id= and url(# have the same suffix (gradient reference integrity)
      const idMatch = svgHtml.match(/id="lobe-icons-grad-0_([a-z0-9]+)"/)
      const urlMatch = svgHtml.match(/url\(#lobe-icons-grad-0_([a-z0-9]+)\)/)
      expect(idMatch![1]).toBe(urlMatch![1])
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

  describe('background and mono class for low-contrast icons', () => {
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

    it('background is CSS-driven (no inline bgColor), uses --bg-tertiary', () => {
      const wrapper = mountIcon({ backend: 'opencode' })
      const svg = wrapper.find('svg')
      // bgColor is no longer inline — background comes from .agent-icon-bg CSS class
      expect(svg.attributes('style')).not.toContain('background')
    })

    it('falls back to CSS --bg-tertiary when needsBg but no bgColor', () => {
      const wrapper = mountIcon({ backend: 'noBgColor' })
      const svg = wrapper.find('svg')
      expect(svg.classes()).toContain('agent-icon-bg')
      expect(svg.attributes('style')).not.toContain('background')
    })

    it('adds monoCssClass when provided', () => {
      const wrapper = mountIcon({ backend: 'opencode' })
      const svg = wrapper.find('svg')
      expect(svg.classes()).toContain('mono-opencode')
    })

    it('does not add monoCssClass when not provided', () => {
      const wrapper = mountIcon({ backend: 'codebuddy' })
      const svg = wrapper.find('svg')
      // No mono-* classes on color icons
      const monoClasses = svg.classes().filter(c => c.startsWith('mono-'))
      expect(monoClasses).toHaveLength(0)
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

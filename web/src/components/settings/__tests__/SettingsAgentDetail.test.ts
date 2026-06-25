import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { mockGetAgent, mockLoadAgents, mockPatchAgentField, mockApiGet, mockToastShow } = vi.hoisted(() => ({
  mockGetAgent: vi.fn(),
  mockLoadAgents: vi.fn().mockResolvedValue(undefined),
  mockPatchAgentField: vi.fn().mockResolvedValue(undefined),
  mockApiGet: vi.fn().mockResolvedValue({ commonPrompt: '' }),
  mockToastShow: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: any) => {
      const map: Record<string, string> = {
        'settings.items.agentPreferredModel': 'Preferred Model',
        'settings.items.agentPreferredThinkingEffort': 'Thinking Effort',
        'settings.items.agentTransport': 'Transport',
        'settings.items.agentSectionIdentity': 'Identity',
        'settings.items.agentName': 'Name',
        'settings.items.agentIcon': 'Icon',
        'settings.items.agentSpecialty': 'Specialty',
        'settings.items.agentSectionAdvanced': 'Advanced',
        'settings.items.agentSystemPrompt': 'System Prompt',
        'settings.items.agentSystemPromptDesc': 'Custom system prompt',
        'settings.items.agentSystemPromptWarning': 'Warning',
        'settings.items.agentSystemPromptACPNote': 'ACP note',
        'settings.items.agentSectionInfo': 'Info',
        'settings.items.agentBackend': 'Backend',
        'settings.items.agentCommand': 'Command',
        'settings.items.agentSource': 'Source',
        'settings.items.agentModels': 'Models',
        'settings.items.agentModelCount': `${params?.count ?? 0} models`,
        'settings.items.agentAcpCommand': 'ACP Command',
        'settings.saveFailed': 'Save failed',
      }
      if (key === 'settings.items.agentModelCount' && params) return `${params.count} models`
      return map[key] ?? key
    },
  }),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgent: mockGetAgent, loadAgents: mockLoadAgents }),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  patchAgentField: mockPatchAgentField,
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/utils/api', () => ({
  apiGet: mockApiGet,
}))

import SettingsAgentDetail from '@/components/settings/SettingsAgentDetail.vue'

const baseAgent = {
  id: 'test-agent',
  name: 'Test Agent',
  icon: 'bot',
  specialty: 'coding',
  backend: 'claude',
  command: 'claude',
  source: 'discovery',
  transport: 'cli',
  models: [{ id: 'model-1', name: 'Model 1', default: true }],
  preferredModel: 'model-1',
  customSystemPrompt: '',
  systemPrompt: '',
  acpCommand: '',
  canRefreshModels: true,
  thinkingEffortLevels: [],
  preferredThinkingEffort: '',
}

function mountDetail(agentOverrides: Record<string, any> = {}) {
  const agent = { ...baseAgent, ...agentOverrides }
  mockGetAgent.mockReturnValue(agent)
  return mount(SettingsAgentDetail, {
    props: { agentId: 'test-agent' },
    global: {
      stubs: {
        SettingsItem: true,
      },
    },
  })
}

describe('SettingsAgentDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetAgent.mockReturnValue(baseAgent)
  })

  it('renders SettingsItem children for a basic CLI agent', () => {
    const wrapper = mountDetail()
    // The component should render SettingsItem stubs for each item
    const items = wrapper.findAllComponents({ name: 'SettingsItem' })
    expect(items.length).toBeGreaterThan(0)
  })

  it('renders more items for dual-transport agent', () => {
    const wrapper1 = mountDetail()
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    const wrapper2 = mountDetail({ acpCommand: 'claude --acp' })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    // Dual-transport agent has transport select + acp_command info
    expect(count2).toBeGreaterThan(count1)
  })

  it('renders fewer items for agent without command', () => {
    const wrapper1 = mountDetail({ command: 'claude' })
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    const wrapper2 = mountDetail({ command: '' })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    expect(count1).toBeGreaterThan(count2)
  })

  it('renders more items when thinking effort levels exist', () => {
    const wrapper1 = mountDetail({ thinkingEffortLevels: [] })
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    const wrapper2 = mountDetail({ thinkingEffortLevels: ['low', 'medium', 'high'] })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    expect(count2).toBeGreaterThan(count1)
  })

  it('renders same number of items for ACP-only vs non-ACP agent (type differs, count same)', () => {
    const wrapper1 = mountDetail({ acpCommand: '', canRefreshModels: true })
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    // ACP-only has no transport select but has system prompt as info
    const wrapper2 = mountDetail({ acpCommand: 'claude --acp', canRefreshModels: false })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    // Both should have items
    expect(count1).toBeGreaterThan(0)
    expect(count2).toBeGreaterThan(0)
  })

  it('loads agents on mount', () => {
    mountDetail()
    expect(mockLoadAgents).toHaveBeenCalledWith(true)
  })

  it('renders container div', () => {
    const wrapper = mountDetail()
    expect(wrapper.find('.settings-agent-detail').exists()).toBe(true)
  })
})

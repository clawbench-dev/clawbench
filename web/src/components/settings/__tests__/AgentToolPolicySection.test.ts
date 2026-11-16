import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import AgentToolPolicySection from '@/components/settings/AgentToolPolicySection.vue'

const patchAgentField = vi.fn()

vi.mock('@/composables/useSettingsConfig', () => ({
    useSettingsConfig: () => ({ patchAgentField }),
}))

// The constants come from useAgents, whose module body registers a WebSocket
// listener via a floating promise. Importing it for real leaks that promise
// into the test run, so the module is mocked down to what this component reads.
vi.mock('@/composables/useAgents', () => ({
    CAP_FS_READ: 'fs.read',
    CAP_FS_WRITE: 'fs.write',
    CAP_EXEC: 'exec',
}))

vi.mock('vue-i18n', async () => {
    const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
    return {
        ...actual,
        useI18n: () => ({ t: (key: string) => key }),
    }
})

/** Find the checkbox whose row label matches a capability label key. */
function checkboxFor(wrapper: ReturnType<typeof mount>, labelKey: string) {
    const rows = wrapper.findAll('.agent-tools__row')
    for (const row of rows) {
        if (row.text().includes(labelKey)) {
            return row.find('input[type="checkbox"]')
        }
    }
    throw new Error(`no row for ${labelKey}`)
}

function mountSection(toolPolicy?: {
    allow: string[]
    maxRounds?: number
    maxToolCalls?: number
    restrictToWorkDir?: boolean
}) {
    return mount(AgentToolPolicySection, {
        props: { agent: { id: 'builtin', toolPolicy } },
    })
}

/**
 * The outside-access switch.
 *
 * Looked up by its label rather than by position: the capability checkboxes
 * share the same row class, and its position among them depends on which tools
 * are granted.
 */
function outsideSwitch(wrapper: ReturnType<typeof mount>) {
    return checkboxFor(wrapper, 'settings.items.toolPolicyOutside')
}

/** The budget inputs, in template order: rounds first, tool calls second. */
function budgetInputs(wrapper: ReturnType<typeof mount>) {
    return wrapper.findAll('.agent-tools__budget input[type="number"]')
}

describe('AgentToolPolicySection', () => {
    beforeEach(() => {
        patchAgentField.mockReset()
        patchAgentField.mockResolvedValue(undefined)
    })

    it('renders every capability unchecked for an empty policy', () => {
        const wrapper = mountSection({ allow: [] })
        const boxes = wrapper.findAll('input[type="checkbox"]')
        expect(boxes).toHaveLength(3)
        for (const box of boxes) {
            expect((box.element as HTMLInputElement).checked).toBe(false)
        }
        // The "no tools" note must be visible: an agent with no grant can only chat.
        expect(wrapper.text()).toContain('settings.items.toolPolicyNoTools')
    })

    it('ticks read when read is granted', () => {
        const wrapper = mountSection({ allow: ['fs.read'] })
        expect((checkboxFor(wrapper, 'settings.items.toolPolicyRead').element as HTMLInputElement).checked).toBe(true)
        expect((checkboxFor(wrapper, 'settings.items.toolPolicyWrite').element as HTMLInputElement).checked).toBe(false)
    })

    it('ticks and locks write when exec is granted', () => {
        const wrapper = mountSection({ allow: ['exec'] })
        const write = checkboxFor(wrapper, 'settings.items.toolPolicyWrite')
        expect((write.element as HTMLInputElement).checked).toBe(true)
        // Locked, because exec implies write and the server stores it closed.
        expect((write.element as HTMLInputElement).disabled).toBe(true)
        // And the consequence is stated where the grant is made.
        expect(wrapper.text()).toContain('settings.items.toolPolicyExecWarning')
    })

    it('sends the closure when exec is ticked', async () => {
        const wrapper = mountSection({ allow: [] })
        await checkboxFor(wrapper, 'settings.items.toolPolicyExec').setValue(true)

        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', {
            allow: ['fs.read', 'fs.write', 'exec'],
        })
    })

    it('unticking read also clears write and exec', async () => {
        const wrapper = mountSection({ allow: ['fs.read', 'fs.write', 'exec'] })
        await checkboxFor(wrapper, 'settings.items.toolPolicyRead').setValue(false)

        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', { allow: [] })
    })

    it('unticking write keeps read but drops exec', async () => {
        const wrapper = mountSection({ allow: ['fs.write'] })
        await checkboxFor(wrapper, 'settings.items.toolPolicyWrite').setValue(false)

        // read is preserved: it was granted implicitly and does not depend on
        // write. Only the dependent (exec) goes with it.
        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', { allow: ['fs.read'] })
    })

    it('preserves budget fields when the grant changes', async () => {
        const wrapper = mount(AgentToolPolicySection, {
            props: {
                agent: {
                    id: 'builtin',
                    toolPolicy: { allow: [], maxRounds: 9, bashTimeoutSec: 30 },
                },
            },
        })
        await checkboxFor(wrapper, 'settings.items.toolPolicyRead').setValue(true)

        // A partial object would make the server treat the budgets as "leave
        // alone" only because they were omitted — but an omitted field is
        // indistinguishable from a cleared one in some patch paths, so the
        // whole policy is sent.
        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', {
            allow: ['fs.read'],
            maxRounds: 9,
            bashTimeoutSec: 30,
        })
    })

    it('surfaces a save failure instead of silently reverting', async () => {
        patchAgentField.mockRejectedValue(new Error('server said no'))
        const wrapper = mountSection({ allow: [] })

        await checkboxFor(wrapper, 'settings.items.toolPolicyRead').setValue(true)
        await vi.waitFor(() => {
            expect(wrapper.text()).toContain('server said no')
        })
    })

    it('shows the stored budgets and the defaults as placeholders', () => {
        const wrapper = mountSection({ allow: [], maxRounds: 40 })
        const [rounds, calls] = budgetInputs(wrapper)

        expect((rounds.element as HTMLInputElement).value).toBe('40')
        // Unset means "server default": the placeholder states it without
        // pinning the agent to today's number.
        expect((calls.element as HTMLInputElement).value).toBe('')
        expect((calls.element as HTMLInputElement).placeholder).toBe('300')
    })

    it('saves a changed round budget with the whole policy', async () => {
        const wrapper = mountSection({ allow: ['fs.read'], maxToolCalls: 50 })
        await budgetInputs(wrapper)[0].setValue('80')

        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', {
            allow: ['fs.read'],
            maxToolCalls: 50,
            maxRounds: 80,
        })
    })

    it('clearing a budget sends 0, which the server reads as "use the default"', async () => {
        const wrapper = mountSection({ allow: [], maxToolCalls: 50 })
        await budgetInputs(wrapper)[1].setValue('')

        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', {
            allow: [],
            maxToolCalls: 0,
        })
    })

    it('rejects a non-positive budget without sending it', async () => {
        const wrapper = mountSection({ allow: [], maxRounds: 40 })
        const input = budgetInputs(wrapper)[0]
        await input.setValue('-5')

        // The field snaps back to the stored value; nothing is persisted.
        expect((input.element as HTMLInputElement).value).toBe('40')
        expect(patchAgentField).not.toHaveBeenCalled()
    })

    it('restores the stored budget when the save fails', async () => {
        patchAgentField.mockRejectedValue(new Error('server said no'))
        const wrapper = mountSection({ allow: [], maxRounds: 40 })
        const input = budgetInputs(wrapper)[0]

        await input.setValue('80')
        await vi.waitFor(() => {
            expect(wrapper.text()).toContain('server said no')
        })
        expect((input.element as HTMLInputElement).value).toBe('40')
    })

    // The outside-access switch is the one setting whose stored polarity is the
    // OPPOSITE of what the checkbox shows (stored: restrict; shown: allow). Both
    // directions are pinned, because a single inverted mapping would silently
    // fence every agent or open every agent.

    it('shows outside access as on when the policy never set it', () => {
        const wrapper = mountSection({ allow: ['fs.read'] })
        // Absent means allowed — the default this shipped with. An unset field
        // must not read as "off", which would look like the fence was enabled.
        expect((outsideSwitch(wrapper).element as HTMLInputElement).checked).toBe(true)
    })

    it('shows outside access as off when the policy restricts to the work dir', () => {
        const wrapper = mountSection({ allow: ['fs.read'], restrictToWorkDir: true })
        expect((outsideSwitch(wrapper).element as HTMLInputElement).checked).toBe(false)
    })

    it('turning outside access off stores the restriction, not the permission', async () => {
        const wrapper = mountSection({ allow: ['fs.read'], maxRounds: 40 })
        await outsideSwitch(wrapper).setValue(false)

        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', {
            allow: ['fs.read'],
            maxRounds: 40,
            restrictToWorkDir: true,
        })
    })

    it('turning outside access back on clears the restriction', async () => {
        const wrapper = mountSection({ allow: ['fs.read'], restrictToWorkDir: true })
        await outsideSwitch(wrapper).setValue(true)

        expect(patchAgentField).toHaveBeenCalledWith('builtin', 'tool_policy', {
            allow: ['fs.read'],
            restrictToWorkDir: false,
        })
    })

    it('hides the outside switch when no tool is granted', () => {
        const wrapper = mountSection({ allow: [] })
        // Nothing for the switch to govern: a live-looking toggle would imply
        // it does something.
        expect(wrapper.text()).not.toContain('settings.items.toolPolicyOutside')
    })

    it('explains that the switch does not cover command execution', () => {
        const wrapper = mountSection({ allow: ['exec'] })
        expect(wrapper.text()).toContain('settings.items.toolPolicyOutsideExecNote')
    })
})

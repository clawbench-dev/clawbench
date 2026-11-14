import { describe, expect, it } from 'vitest'
import { GATE_SCRIPT_VARS, gateScriptVarsFor } from '../gateScriptVars'

describe('gateScriptVars', () => {
    it('lists every variable exactly once', () => {
        const names = GATE_SCRIPT_VARS.map((v) => v.name)
        expect(new Set(names).size).toBe(names.length)
        // Every name is namespaced: a bare name could collide with the user's
        // own variables in the child process.
        expect(names.every((n) => n.startsWith('CLAWBENCH_'))).toBe(true)
    })

    it('always includes the common variables', () => {
        for (const mode of ['cron', 'email', 'event', undefined]) {
            const names = gateScriptVarsFor(mode).map((v) => v.name)
            expect(names).toContain('CLAWBENCH_TRIGGER')
            expect(names).toContain('CLAWBENCH_TASK_ID')
        }
    })

    it('scopes mail and event variables to their trigger mode', () => {
        const mail = gateScriptVarsFor('email').map((v) => v.name)
        expect(mail).toContain('CLAWBENCH_MAIL_SUBJECT')
        expect(mail).not.toContain('CLAWBENCH_EVENT_TYPE')

        const event = gateScriptVarsFor('event').map((v) => v.name)
        expect(event).toContain('CLAWBENCH_EVENT_TYPE')
        expect(event).toContain('CLAWBENCH_PIPELINE_STATUS')
        expect(event).not.toContain('CLAWBENCH_MAIL_SUBJECT')

        const cron = gateScriptVarsFor('cron').map((v) => v.name)
        expect(cron).not.toContain('CLAWBENCH_MAIL_SUBJECT')
        expect(cron).not.toContain('CLAWBENCH_EVENT_TYPE')
    })

    it('falls back to the common set for an unknown mode', () => {
        // The backend derives the trigger from the task's mode and falls back
        // to cron; the form must show the same set rather than nothing.
        expect(gateScriptVarsFor('something-else').map((v) => v.name))
            .toEqual(gateScriptVarsFor('cron').map((v) => v.name))
    })
})

import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingRegenerateSheet from '../MeetingRegenerateSheet.vue'
import type { RegenPlan } from '@/composables/useMeetingHistory'

const minutesPlan: RegenPlan = {
  target: 'minutes',
  steps: [
    { kind: 'transcript', mode: 'render', reason: 'missing' },
    { kind: 'revised', mode: 'model', reason: 'stale' },
    { kind: 'minutes', mode: 'model', reason: 'target' },
  ],
  warnings: ['overwrite_transcript'],
  blocked: null,
}

const asrPlan: RegenPlan = {
  target: 'transcript',
  steps: [{ kind: 'transcript', mode: 'asr', reason: 'target' }],
  warnings: ['overwrite_transcript', 'mixed_audio_only'],
  blocked: null,
}

function mountSheet(plan: RegenPlan | null = minutesPlan, over: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingRegenerateSheet, {
    props: {
      open: true,
      target: plan?.target ?? 'minutes',
      plan,
      loading: false,
      error: '',
      includeDownstream: true,
      ...over,
    },
    global: {
      plugins: [i18n],
      stubs: {
        BottomSheet: {
          props: ['open', 'title'],
          template: '<div class="bs"><div class="bs-title">{{ title }}</div><slot /><slot name="footer" /></div>',
        },
        LoadingIndicator: true,
      },
    },
  })
}

describe('MeetingRegenerateSheet', () => {
  it('renders the plan in order with modes and reasons', () => {
    const w = mountSheet()
    const text = w.text()
    expect(text).toContain(enMessages.meeting.detail.regen.plan)
    expect(text).toContain(enMessages.meeting.detail.tabTranscript)
    expect(text).toContain(enMessages.meeting.detail.regen.modeRender)
    expect(text).toContain(enMessages.meeting.detail.regen.reasonMissing)
    expect(text).toContain(enMessages.meeting.detail.tabRevised)
    expect(text).toContain(enMessages.meeting.detail.regen.reasonStale)
    expect(text).toContain(enMessages.meeting.detail.tabMinutes)
    expect(text).toContain(enMessages.meeting.detail.regen.reasonTarget)
    expect(text).toContain(enMessages.meeting.detail.regen.overwriteTranscript)
  })

  it('does not offer downstream for the last artifact', () => {
    const w = mountSheet()
    expect(w.find('[data-role="regen-downstream"]').exists()).toBe(false)
  })

  it('offers downstream for the transcript and emits the new value', async () => {
    const w = mountSheet(asrPlan)
    const checkbox = w.find('[data-role="regen-downstream"]')
    expect(checkbox.exists()).toBe(true)
    await checkbox.setValue(false)
    expect(w.emitted('update:includeDownstream')?.[0]).toEqual([false])
  })

  it('uses recognition wording for an asr plan', () => {
    const w = mountSheet(asrPlan)
    expect(w.find('.bs-title').text()).toBe(enMessages.meeting.detail.regen.titleAsr)
    expect(w.text()).toContain(enMessages.meeting.detail.regen.modeAsr)
    expect(w.find('[data-action="regen-start"]').text()).toBe(enMessages.meeting.detail.regen.startAsr)
    expect(w.text()).toContain(enMessages.meeting.detail.regen.mixedAudioOnly)
  })

  it('disables the start button when the plan is blocked', () => {
    const w = mountSheet({
      ...minutesPlan,
      blocked: { code: 'no_source', message: 'no source' },
    })
    expect(w.text()).toContain(enMessages.meeting.detail.regen.blockedNoSource)
    expect(w.find('[data-action="regen-start"]').attributes('disabled')).toBeDefined()
  })

  it('shows a preparing state before the plan arrives', () => {
    const w = mountSheet(null, { loading: true })
    expect(w.text()).toContain(enMessages.meeting.detail.regen.preparing)
    expect(w.find('[data-action="regen-start"]').attributes('disabled')).toBeDefined()
  })

  it('emits confirm from the start button', async () => {
    const w = mountSheet()
    await w.find('[data-action="regen-start"]').trigger('click')
    expect(w.emitted('confirm')).toHaveLength(1)
  })
})

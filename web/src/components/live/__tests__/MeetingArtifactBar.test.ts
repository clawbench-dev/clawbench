import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import enMessages from '@/i18n/locales/en'
import MeetingArtifactBar from '../MeetingArtifactBar.vue'

const NOW = 1_700_000_000_000

function mountBar(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enMessages } })
  return mount(MeetingArtifactBar, {
    props: {
      kind: 'minutes',
      state: { hasContent: true, jobStatus: 'idle', contentAt: NOW, stale: false },
      generation: null,
      hasAudio: true,
      ...props,
    },
    global: { plugins: [i18n], stubs: { LoadingIndicator: true } },
  })
}

describe('MeetingArtifactBar', () => {
  it('shows the generated time and offers a regeneration', () => {
    const w = mountBar()
    expect(w.text()).toContain(enMessages.meeting.detail.artifact.generatedAt.replace('{time}', ''))
    expect(w.find('[data-action="meeting-artifact-regenerate"]').text())
      .toBe(enMessages.meeting.detail.artifact.regenerate)
  })

  it('offers "generate" when the artifact is empty', () => {
    const w = mountBar({
      state: { hasContent: false, jobStatus: 'idle', contentAt: 0, stale: false },
    })
    expect(w.text()).toContain(enMessages.meeting.detail.artifact.notGenerated)
    expect(w.find('[data-action="meeting-artifact-regenerate"]').text())
      .toBe(enMessages.meeting.detail.artifact.generate)
  })

  it('calls the transcript action "re-transcribe" when a recording exists', () => {
    const w = mountBar({
      kind: 'transcript',
      hasAudio: true,
      state: { hasContent: false, jobStatus: 'idle', contentAt: 0, stale: false },
    })
    expect(w.find('[data-action="meeting-artifact-regenerate"]').text())
      .toBe(enMessages.meeting.detail.artifact.reasr)
  })

  it('falls back to "generate" for a transcript with only segments', () => {
    const w = mountBar({
      kind: 'transcript',
      hasAudio: false,
      state: { hasContent: false, jobStatus: 'idle', contentAt: 0, stale: false },
    })
    expect(w.find('[data-action="meeting-artifact-regenerate"]').text())
      .toBe(enMessages.meeting.detail.artifact.generate)
  })

  it('marks a stale artifact and offers an update', () => {
    const w = mountBar({
      state: { hasContent: true, jobStatus: 'idle', contentAt: NOW, stale: true },
    })
    expect(w.text()).toContain(enMessages.meeting.detail.artifact.stale.replace('{upstream}', ''))
    expect(w.find('[data-action="meeting-artifact-regenerate"]').text())
      .toBe(enMessages.meeting.detail.artifact.update)
  })

  it('shows the running step with progress and offers cancel', async () => {
    const w = mountBar({
      generation: { step: 'minutes', remaining: [], detail: '3/8', startedAt: Date.now() - 5_000 },
    })
    expect(w.text()).toContain('Regenerating')
    expect(w.text()).toContain(enMessages.meeting.detail.tabMinutes)
    expect(w.text()).toContain('3/8')
    const cancel = w.find('[data-action="meeting-artifact-cancel"]')
    expect(cancel.text()).toBe(enMessages.meeting.detail.artifact.cancel)
    await cancel.trigger('click')
    expect(w.emitted('cancel')).toHaveLength(1)
  })

  it('says a queued step is waiting, not running', () => {
    const w = mountBar({
      generation: { step: 'transcript', remaining: ['revised', 'minutes'], startedAt: Date.now() },
    })
    expect(w.text()).toContain(enMessages.meeting.detail.artifact.waiting.replace('{kind}', ''))
    expect(w.find('[data-action="meeting-artifact-cancel"]').exists()).toBe(true)
  })

  it('localizes an interrupted failure and offers a retry', () => {
    const w = mountBar({
      state: {
        hasContent: true,
        jobStatus: 'failed',
        errorCode: 'interrupted',
        error: 'interrupted',
        contentAt: NOW,
        stale: false,
      },
    })
    expect(w.text()).toContain(enMessages.meeting.detail.artifact.interrupted)
    expect(w.text()).toContain(enMessages.meeting.detail.artifact.failedKeepOld)
    expect(w.find('[data-action="meeting-artifact-regenerate"]').text())
      .toBe(enMessages.meeting.detail.artifact.retry)
  })

  it('disables the action when there is no source', () => {
    const w = mountBar({
      state: { hasContent: false, jobStatus: 'idle', contentAt: 0, stale: false },
      blocked: 'No source available',
    })
    expect(w.text()).toContain('No source available')
    expect(w.find('[data-action="meeting-artifact-regenerate"]').attributes('disabled')).toBeDefined()
  })

  it('emits regenerate from the action button', async () => {
    const w = mountBar()
    await w.find('[data-action="meeting-artifact-regenerate"]').trigger('click')
    expect(w.emitted('regenerate')).toHaveLength(1)
  })

  it('ignores a click on a disabled action', async () => {
    const w = mountBar({
      state: { hasContent: false, jobStatus: 'idle', contentAt: 0, stale: false },
      blocked: 'No source available',
    })
    const button = w.find('[data-action="meeting-artifact-regenerate"]')
    await button.trigger('click')
    expect(w.emitted('regenerate')).toBeUndefined()
  })
})

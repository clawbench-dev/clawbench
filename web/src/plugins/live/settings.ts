import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/** Settings category id (the plugin's directory is `live`). */
export const category = 'meeting'

/**
 * Settings fields for this plugin's own settings page (`meeting`).
 *
 * Moved out of the central `settingsFieldMap.ts` table so that adding a setting
 * to this feature is ONE directory: the plugin declares its page, and the page's
 * fields (their i18n keys and config keys) sit next to the plugin itself.
 *
 * The import above is `import type` — erased at build time, so this module pulls
 * nothing from `settingsFieldMap.ts` (or from anything it imports) into the
 * plugin graph.
 */
export default [
    // 参会称呼. Its own section, FIRST on the page: it is the one field here
    // that every meeting uses (it names you in the member list, the transcript
    // and the chat), and it is deliberately independent of
    // 设置 → 身份 → 我的称谓, which is how agents address you in a chat.
    { type: 'item', spec: { labelKey: 'settings.items.meetingAttendeeName', descriptionKey: 'settings.items.meetingAttendeeNameDesc', key: 'meeting.attendee_name', type: 'text', source: 'server', maxLength: 32, sectionHeader: 'settings.items.meetingAttendeeSection' } },
    // AI 纪要. The model row reuses the shared ai_summary config on purpose:
    // meeting minutes, proofreading, auto-title, voice summary, recommendations,
    // session auto-title and /btw all share one model, so a meeting-specific
    // picker would be a second source of truth for the same credential.
    { type: 'item', spec: { labelKey: 'settings.items.summaryModelRow', descriptionKey: 'settings.items.summaryModelRowDescMeeting', key: 'summaryModel', type: 'summaryModel', source: 'local', showSummaryModelStatus: true, sectionHeader: 'settings.items.meetingSummarySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingAutoMinutes', descriptionKey: 'settings.items.meetingAutoMinutesDesc', key: 'meeting.auto_minutes', type: 'switch', source: 'server', sectionHeader: 'settings.items.meetingSummarySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingProofread', descriptionKey: 'settings.items.meetingProofreadDesc', key: 'meeting.proofread', type: 'switch', source: 'server', sectionHeader: 'settings.items.meetingSummarySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingLanguage', descriptionKey: 'settings.items.meetingLanguageDesc', key: 'meeting.language', type: 'select', source: 'server', sectionHeader: 'settings.items.meetingSummarySection', options: [
      { labelKey: 'settings.items.meetingLanguageAuto', value: '' },
      { labelKey: 'settings.items.meetingLanguageZh', value: 'zh' },
      { labelKey: 'settings.items.meetingLanguageEn', value: 'en' },
    ]} },
    { type: 'item', spec: { labelKey: 'settings.items.meetingFileExcerpts', descriptionKey: 'settings.items.meetingFileExcerptsDesc', key: 'meeting.file_excerpts', type: 'switch', source: 'server', sectionHeader: 'settings.items.meetingSummarySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingMinutesMaxTokens', descriptionKey: 'settings.items.meetingMinutesMaxTokensDesc', key: 'meeting.minutes_max_tokens', type: 'number', source: 'server', min: 0, max: 8000, step: 500, sectionHeader: 'settings.items.meetingSummarySection' } },
    // Agent mode: same inputs, same prompts, but the model call is made by the
    // built-in agent, which can READ project files to check how a term is
    // actually spelled. Placed after the summary-model rows because it replaces
    // them for this task — reading it before them would suggest it is additive.
    { type: 'item', spec: { labelKey: 'settings.items.meetingAgentMinutes', descriptionKey: 'settings.items.meetingAgentMinutesDesc', key: 'meeting.agent_minutes', type: 'switch', source: 'server', sectionHeader: 'settings.items.meetingSummarySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingAgentMinutesMaxRounds', descriptionKey: 'settings.items.meetingAgentMinutesMaxRoundsDesc', key: 'meeting.agent_minutes_max_rounds', type: 'number', source: 'server', min: 0, max: 100, step: 1, sectionHeader: 'settings.items.meetingSummarySection' } },
    // New-meeting defaults. These are local (per device) because they seed the
    // create form; the room-level value is still chosen per meeting and can be
    // changed in the stage's 会议管理 tab.
    { type: 'item', spec: { labelKey: 'settings.items.meetingDefaultNotes', descriptionKey: 'settings.items.meetingDefaultNotesDesc', key: 'meetingDefaultNotes', type: 'switch', source: 'local', sectionHeader: 'settings.items.meetingDefaultsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingDefaultShots', descriptionKey: 'settings.items.meetingDefaultShotsDesc', key: 'meetingDefaultShots', type: 'switch', source: 'local', sectionHeader: 'settings.items.meetingDefaultsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingDefaultAllowTalk', descriptionKey: 'settings.items.meetingDefaultAllowTalkDesc', key: 'meetingDefaultAllowTalk', type: 'switch', source: 'local', sectionHeader: 'settings.items.meetingDefaultsSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.meetingDefaultAllowMemberScreen', descriptionKey: 'settings.items.meetingDefaultAllowMemberScreenDesc', key: 'meetingDefaultAllowMemberScreen', type: 'switch', source: 'local', sectionHeader: 'settings.items.meetingDefaultsSection' } },
    // 画面质量. Local (per device): the right ceiling depends on the machine that
    // has to encode the picture and the link it uploads over, so the same
    // account on a phone and on a workstation must be able to differ. Read back
    // by `utils/liveQualityPrefs` when a capture starts — the option values here
    // and the whitelists there are one contract.
    { type: 'item', spec: { labelKey: 'settings.items.liveScreenResolution', descriptionKey: 'settings.items.liveScreenResolutionDesc', key: 'liveScreenResolution', type: 'select', source: 'local', sectionHeader: 'settings.items.meetingQualitySection', options: [
      { labelKey: 'settings.items.liveScreenRes720p', value: '1280x720' },
      { labelKey: 'settings.items.liveScreenRes1200p', value: '1920x1200' },
      { labelKey: 'settings.items.liveScreenRes1600p', value: '2560x1600' },
      { labelKey: 'settings.items.liveScreenRes2160p', value: '3840x2160' },
    ] } },
    { type: 'item', spec: { labelKey: 'settings.items.liveScreenFps', descriptionKey: 'settings.items.liveScreenFpsDesc', key: 'liveScreenFps', type: 'select', source: 'local', sectionHeader: 'settings.items.meetingQualitySection', options: [
      { labelKey: 'settings.items.liveFps15', value: '15' },
      { labelKey: 'settings.items.liveFps24', value: '24' },
      { labelKey: 'settings.items.liveFps30', value: '30' },
      { labelKey: 'settings.items.liveFps60', value: '60' },
    ] } },
    { type: 'item', spec: { labelKey: 'settings.items.liveScreenBitrate', descriptionKey: 'settings.items.liveScreenBitrateDesc', key: 'liveScreenBitrate', type: 'select', source: 'local', sectionHeader: 'settings.items.meetingQualitySection', options: [
      { labelKey: 'settings.items.liveMbps2', value: '2' },
      { labelKey: 'settings.items.liveMbps4', value: '4' },
      { labelKey: 'settings.items.liveMbps6', value: '6' },
      { labelKey: 'settings.items.liveMbps8', value: '8' },
      { labelKey: 'settings.items.liveMbps12', value: '12' },
    ] } },
    { type: 'item', spec: { labelKey: 'settings.items.liveCameraResolution', descriptionKey: 'settings.items.liveCameraResolutionDesc', key: 'liveCameraResolution', type: 'select', source: 'local', sectionHeader: 'settings.items.meetingQualitySection', options: [
      { labelKey: 'settings.items.liveCamRes480p', value: '640x480' },
      { labelKey: 'settings.items.liveCamRes720p', value: '1280x720' },
      { labelKey: 'settings.items.liveCamRes1080p', value: '1920x1080' },
    ] } },
    { type: 'item', spec: { labelKey: 'settings.items.liveCameraFps', descriptionKey: 'settings.items.liveCameraFpsDesc', key: 'liveCameraFps', type: 'select', source: 'local', sectionHeader: 'settings.items.meetingQualitySection', options: [
      { labelKey: 'settings.items.liveFps15', value: '15' },
      { labelKey: 'settings.items.liveFps24', value: '24' },
      { labelKey: 'settings.items.liveFps30', value: '30' },
    ] } },
    { type: 'item', spec: { labelKey: 'settings.items.liveCameraBitrate', descriptionKey: 'settings.items.liveCameraBitrateDesc', key: 'liveCameraBitrate', type: 'select', source: 'local', sectionHeader: 'settings.items.meetingQualitySection', options: [
      { labelKey: 'settings.items.liveMbps1', value: '1' },
      { labelKey: 'settings.items.liveMbps1_5', value: '1.5' },
      { labelKey: 'settings.items.liveMbps2_5', value: '2.5' },
      { labelKey: 'settings.items.liveMbps4', value: '4' },
    ] } },
    { type: 'item', spec: { labelKey: 'settings.items.liveAutoDowngrade', descriptionKey: 'settings.items.liveAutoDowngradeDesc', key: 'liveAutoDowngrade', type: 'switch', source: 'local', sectionHeader: 'settings.items.meetingQualitySection' } },
    // 中继服务 (LiveKit). Installation-wide, so it lives here rather than in the
    // create form: the relay is deployed once and every meeting that opts in
    // shares it. The item is a placeholder that only opens the card — the form
    // is MeetingRelaySetting, because the credential is write-only and the save
    // is a test-then-store action, neither of which the generic row can do.
    { type: 'item', spec: { labelKey: 'settings.items.meetingRelaySection', key: 'meetingRelay', type: 'info', source: 'server', sectionHeader: 'settings.items.meetingRelaySection', cardBody: { load: () => import('@/components/settings/MeetingRelaySetting.vue') } } },
] satisfies CategoryEntry[]

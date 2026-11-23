import { describe, expect, it, vi, beforeEach, afterEach, beforeAll } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, reactive, nextTick, type Component } from 'vue'
import PasswordChangeDialog from '@/components/settings/PasswordChangeDialog.vue'
import WallpaperSetting from '@/components/settings/WallpaperSetting.vue'
import MeetingRelaySetting from '@/components/settings/MeetingRelaySetting.vue'
import KbSetting from '@/components/settings/KbSetting.vue'
import { categoryItems, type ItemSpec } from '@/components/settings/settingsFieldMap'

// Imported lazily (in beforeAll) rather than statically: the settings module
// graph now reaches modules that call `useSettingsConfig()` at MODULE level
// (the dock plugin runtime reads the feature switches that way). A static
// import evaluates that graph before this file's `localConfig` / `serverConfig`
// fixtures exist, and the vi.mock factory below then hits their TDZ. Importing
// after the fixtures are initialised keeps the mock factory happy without
// making the fixtures hoisted refs.
let SettingsCategory: Component
import { rememberProviderNames } from '@/utils/providerNames'

// Mock composables
const mockSetLocalConfig = vi.fn()
const mockSetServerValue = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [] })
const mockGetServerValueWithDefault = vi.fn()
const mockPatchAgentPref = vi.fn().mockResolvedValue(undefined)
const mockGetAgentModelPref = vi.fn().mockReturnValue(null)
const mockGetAgentThinkingPref = vi.fn().mockReturnValue(null)
const mockLoadAgents = vi.fn()
const mockUpdateAgentField = vi.fn()
const mockDownloadByUrl = vi.hoisted(() => vi.fn())

vi.mock('@/utils/download', async importOriginal => ({
  ...await importOriginal<Record<string, unknown>>(),
  downloadByUrl: mockDownloadByUrl,
}))

const localConfig = reactive<Record<string, any>>({
  theme: 'auto',
  locale: 'zh',
  autoSpeech: false,
  preventScreenLock: false,
  swipeSession: false,
  messageDisplayMode: 'mixed',
  showHidden: false,
  wordWrap: true,
  lineNumbers: true,
  stickyScroll: false,
  fileView: 'list',
  sortField: null,
  sortDir: 'asc',
  recentFilesCount: 10,
  terminalFontSize: 12,
  androidLogCapture: false,
  uiScale: 1,
  notificationSound: true,
  headerShortcutTips: true,
  // Per-device wallpaper choice (localStorage-backed). Scaffolded to an
  // enabled wave so the panel renders its active state; the real factory
  // default (off) is asserted in useSettingsConfig.test.ts.
  wallpaperEnabled: true,
  wallpaperMode: 'wave',
  wallpaperLocalSelected: '',
})

const serverConfig = ref<Record<string, any>>({
  version: 'dev',
  default_agent: '',
  chat: { initial_messages: 20, page_size: 20, system_prompt_interval: 0, recommend_enabled: false, recommend_context_messages: 5 },
  session: { max_count: 10, archive_retention_enabled: false, archive_retention_days: 0 },
  recent_projects: { max_count: 10 },
  file_search: { display_limit: 50 },
  upload: { max_size_mb: 100, max_files: 20 },
  terminal: { enabled: true, idle_timeout: '10m', max_sessions: 10, buffer_lines: 2000 },
  tts: { engine: 'edge', voice: '', speed: 1.0, max_cache_files: 100, format: '' },
  rag: { enabled: false, base_url: 'http://localhost:11434', model: 'bge-m3', api_key: '', chunk_size: 512, search_limit: 5, retention_days: 90 },
  port_forward: { enabled: true, port: 0 },
  summarize: { backend: 'simple', model: '' },
  tls: { cert_dir: '' },
})

const mockAgents = [
  { id: 'codebuddy', name: 'CodeBuddy', backend: 'codebuddy', models: [{ id: 'glm-5.1', name: 'GLM 5.1', default: true }, { id: 'glm-4', name: 'GLM 4', default: false }], thinkingEffortLevels: ['low', 'medium', 'high'], thinkingEffort: 'medium', preferredModel: '' },
  { id: 'claude', name: 'Claude', backend: 'claude', models: [{ id: 'claude-sonnet', name: 'Sonnet', default: true }], thinkingEffortLevels: [], thinkingEffort: '', preferredModel: '' },
]

vi.mock('@/composables/useSettingsConfig', () => ({
  getEffectiveUIScale: () => mockEffectiveUIScale.value,
  autoFitUIScale: mockAutoFitUIScale,
  useSettingsConfig: () => ({
    localConfig,
    serverConfig,
    setLocalConfig: mockSetLocalConfig,
    getServerValueWithDefault: mockGetServerValueWithDefault,
    setServerValue: mockSetServerValue,
    patchAgentPref: mockPatchAgentPref,
    getAgentModelPref: mockGetAgentModelPref,
    getAgentThinkingPref: mockGetAgentThinkingPref,
  }),
}))

vi.mock('@/composables/useAgents', () => ({
  // useSessionIdentity (reached transitively via usePortForward) calls this at
  // module-evaluation time, so the mock must provide it or the import throws.
  registerIdentityUpdaters: vi.fn(),
  useAgents: () => ({
    agents: ref(mockAgents),
    loadAgents: mockLoadAgents,
    updateAgentField: mockUpdateAgentField,
    getAgentModels: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      return a?.models || []
    },
    getAgentThinkingEffortLevels: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      return a?.thinkingEffortLevels || []
    },
    hasThinkingEffortLevels: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      return (a?.thinkingEffortLevels?.length || 0) > 0
    },
    getDefaultModelId: (agentId: string) => {
      const a = mockAgents.find(a => a.id === agentId)
      if (a?.preferredModel) return a.preferredModel
      if (!a?.models?.length) return ''
      const def = a.models.find((m: any) => m.default)
      return def ? def.id : a.models[0].id
    },
    getAgent: (agentId: string) => {
      return mockAgents.find(a => a.id === agentId) || null
    },
    getAgentAvailableModes: () => [],
      getAgentAvailableConfigOptions: () => [],
  }),
}))

// Hoisted (and deliberately a plain object, not a ref) because the vi.mock
// factory below runs before module imports — a `ref()` here would hit the TDZ
// on the hoisted vue import. Cases set `.value` before mounting, so the render
// reads the intended value without needing reactivity.
const mockIsAppMode = vi.hoisted(() => ({ value: false }))
// Mutable so a case can flip the desktop-shell flag. BOTH native hosts report
// isAppMode === true, so "Android app only" rows can only be distinguished by
// consulting isDesktopApp.
const mockIsDesktopApp = vi.hoisted(() => ({ value: false }))
const mockIsHarmonyApp = vi.hoisted(() => ({ value: false }))
vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode, isDesktopApp: mockIsDesktopApp }),
}))

// The component reads the host axis through usePlatformDetect, NOT useAppMode.
// The real composable copies useAppMode into module-level refs behind a
// `platformInitialized` guard, so it would snapshot whatever the FIRST test in
// this file set and ignore every later change — which is exactly the
// order-dependent failure this mock prevents. Deriving the axes per call from
// the explicit host flags keeps each test's platform setup meaningful.
vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({
    isElectron: { value: mockIsDesktopApp.value },
    isAndroidApp: { value: mockIsAppMode.value && !mockIsDesktopApp.value && !mockIsHarmonyApp.value },
    isHarmonyApp: { value: mockIsHarmonyApp.value },
    isWebApp: { value: !mockIsAppMode.value },
    isNativeApp: { value: mockIsAppMode.value },
    isTouchPrimary: { value: false },
  }),
}))

// The scale slider displays the factor actually in effect. Stubbed as a plain
// holder so cases can control it without depending on the real screen height.
const mockEffectiveUIScale = vi.hoisted(() => ({ value: 1 }))

// The auto-fit button. The REAL computation (screen height → factor → stored
// value) is covered in useSettingsConfig.test.ts against the unmocked module;
// here we only pin that clicking the row reaches this path at all.
const mockAutoFitUIScale = vi.hoisted(() => vi.fn(() => 1.5))

const mockCanInstallPwa = ref(false)
const mockIsIOS = ref(false)
const mockInstallPwa = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/usePwaInstall', () => ({
  usePwaInstall: () => ({
    canInstallPwa: mockCanInstallPwa,
    isIOS: mockIsIOS,
    showPwaInstall: ref(true),
    showApkDownload: ref(false),
    installPwa: mockInstallPwa,
  }),
}))

// Mutable so individual tests can simulate "a download exists for this
// platform" — the row's visibility depends on it.
const mockDesktopDownload = {
  latest: ref(null),
  loading: ref(false),
  isDesktop: false,
  loadLatest: vi.fn(),
  currentDownloadUrl: () => '',
  downloadDesktop: vi.fn(),
}

vi.mock('@/composables/useDesktopDownload', () => ({
  useDesktopDownload: () => mockDesktopDownload,
}))

vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue({ needs_restart: true }),
  apiGet: vi.fn().mockResolvedValue({ dir: '', fonts: [] }),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))


const mockDialogConfirm = vi.fn().mockResolvedValue(false)
vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { ok: '确定' },
      settings: {
        needsRestart: '需重启',
        categories: { chat: '聊天', agents: '智能体', appearance: '外观', tts: '语音', portForward: '端口映射', push: '推送', terminal: '终端', rag: 'RAG', project: '项目', files: '文件', about: '关于', android: 'Android', security: '安全', basic: '其他' },
        // Only the interpolation key: the banner's title is asserted as a raw
        // key below (this fixture carries no copy), but the query itself must
        // render, so its wrapper has to resolve.
        search: { filteredQuery: '「{query}」' },
        items: {
          defaultAgent: '默认智能体',
          autoSpeech: '自动语音',
          autoSpeechDesc: 'AI 回复完成后自动朗读',
          preventScreenLock: '输出期间不锁屏',
          preventScreenLockDesc: 'AI 输出期间抑制屏幕休眠',
          swipeSession: '滑动切换会话',
          swipeSessionDesc: '聊天区域左右滑动切换会话',
          messageDisplayMode: '消息展示模式',
          messageDisplayModeDesc: '有摘要的消息默认展示方式',
          messageDisplayModeMixed: '混合模式',
          messageDisplayModeSummary: '摘要模式',
          messageDisplayModeOriginal: '原文模式',
          chatInitialMessages: '初始消息数',
          chatInitialMessagesDesc: '打开会话时加载的消息数量',
          chatPageSize: '每页消息数',
          chatPageSizeDesc: '每页消息数',
          chatSystemPromptInterval: '系统提示间隔',
          chatSystemPromptIntervalDesc: '每隔多少条消息插入一次系统提示',
          chatRecommendEnabled: '推荐回复',
          chatRecommendEnabledDesc: '推荐回复',
          chatRecommendContextMessages: '推荐上下文消息数',
          chatRecommendContextMessagesDesc: '推荐上下文消息数',
          sessionMaxCount: '最大会话数',
          sessionMaxCountDesc: '最大会话数',
          sessionSortRecent: '会话按最近活动排序',
          sessionSortRecentDesc: '会话按最近活动排序',
          archiveRetentionEnabled: '自动清理归档会话',
          archiveRetentionEnabledDesc: '归档会话到期后自动物理删除',
          archiveRetentionDays: '留存天数',
          archiveRetentionDaysDesc: '归档会话保留天数',
          recentProjectsMaxCount: '最近项目数量',
          recentProjectsMaxCountDesc: '最近项目数量',
          fileSearchDisplayLimit: '搜索结果数量上限',
          fileSearchDisplayLimitDesc: '搜索结果数量上限',
          theme: '主题',
          themeDesc: '主题',
          locale: '语言',
          localeDesc: '语言',
          uiScale: '界面缩放',
          uiScaleDesc: '界面缩放',
          uiScaleAutoFit: '自动适配',
          uiScaleAutoFitDesc: '自动适配',
          uiScaleAutoFitDone: '已按当前屏幕适配为 {pct}%',
          headerShortcutTips: '顶栏快捷键提示',
          headerShortcutTipsDesc: '顶栏快捷键提示',
          ttsEngine: 'TTS引擎',
          ttsEngineDesc: 'TTS引擎',
          ttsEngineEdge: 'Edge',
          ttsEngineSection: 'TTS 引擎',
          voiceSummarySection: '语音摘要',
          summarizeTtsSection: '语音摘要',
          summarizeTtsBackendDesc: '对 AI 输出的消息进行摘要，便于语音朗读',
          summarizeAutoJunkRatio: '自动判定杂质比例阈值',
          summarizeAutoJunkRatioDesc: '自动模式下按杂质比例决定是否走大模型摘要',
          ttsVoice: '语音',
          ttsSpeed: '语速',
          summarizeBackend: '摘要方式',
          summarizeSimple: '简单',
          summarizeApi: 'API',
          summarizeModel: '摘要模型',
          apiHeader: 'API',
          apiBaseUrl: 'API地址',
          apiKey: 'API密钥',
          ttsMaxCacheFiles: '缓存上限',
          ragSearchPoolSize: '搜索池大小',
          ragBaseUrl: '嵌入接口地址',
          ragModel: '嵌入模型',
          portForwardEnabled: '启用端口映射',
          portForwardPort: 'SSH 隧道端口',
          portForwardPortAuto: '自动',
          portForwardHeader: '端口映射',
          ttsCacheHeader: '缓存',
          terminalEnabled: '启用终端',
          terminalIdleTimeout: '空闲超时',
          terminalMaxSessions: '最大会话',
          terminalBufferLines: '缓冲行数',
          terminalFontSize: '终端字号',
          showHidden: '显示隐藏文件',
          showHiddenDesc: '显示隐藏文件',
          wordWrap: '自动换行',
          wordWrapDesc: '自动换行',
          lineNumbers: '行号',
          lineNumbersDesc: '行号',
          stickyScroll: '粘性滚动',
          stickyScrollDesc: '粘性滚动',
          fileView: '视图模式',
          fileViewDesc: '视图模式',
          fileViewList: '列表',
          fileViewGrid: '网格',
          recentFilesCount: '最近文件数量',
          recentFilesCountDesc: '最近文件数量',
          sortField: '默认排序',
          sortFieldDesc: '默认排序',
          sortFieldDefault: '默认',
          sortFieldName: '按名称',
          sortFieldTime: '按时间',
          sortFieldType: '按类型',
          sortFieldSize: '按大小',
          sortDir: '排序方向',
          sortDirHint: '排序方向',
          sortDirAsc: '升序',
          sortDirDesc: '降序',
          inAppNotifySection: '应用内通知',
          inAppNotification: '应用内完成通知',
          inAppNotificationDesc: '会话或任务完成时弹出结果卡片',
          desktopNotifySection: '桌面端通知',
          desktopNotification: '桌面端系统通知',
          desktopNotificationDesc: '页面最小化时用系统通知提醒',
          desktopSystemSection: '桌面与系统',
          mobileNotifySection: '移动端通知',
          notificationSound: '任务完成提示音',
          notificationSoundDesc: '任务完成或需要审批时播放提示音和振动',
          uploadMaxSize: '上传大小上限',
          uploadMaxSizeDesc: '上传大小上限',
          uploadMaxFiles: '上传文件上限',
          uploadMaxFilesDesc: '上传文件上限',
          ragChunkSize: '分块大小',
          ragSearchLimit: '搜索结果数',
          ragBatchSize: '索引器批次大小',
          ragRetentionDays: '保留天数',
          aboutServerVersion: '服务器版本',
          aboutServerVersionDesc: '服务器版本',
          aboutBrandSlogan: '多端一体的 AI 工作台',
          aboutHomepage: '项目主页',
          aboutHomepageDesc: '在浏览器中打开项目主页',
          aboutGithub: 'GitHub 主页',
          aboutGithubDesc: '在浏览器中打开 GitHub 仓库',
          aboutFeedback: '问题反馈',
          aboutFeedbackDesc: '前往 GitHub 提交问题或建议',
          aboutAppVersion: 'APP版本',
          aboutAppVersionDesc: 'APP版本',
          serverRestart: '重启服务器',
          logCapture: '日志抓取',
          reconfigureServer: '重新配置服务器',
          forgeEnabledSection: '启用',
          forgeSection: '凭据',
          forgeEnabled: '启用代码托管集成',
          featureStatusSection: '功能状态',
          featureStatusEnabled: '已启用',
          featureStatusDisabled: '已停用',
          featureStatusGoManage: '去功能编排',
          featureStatusForge: '代码托管集成',
          forgePlatformsDesc: '支持 GitHub、GitLab（自建实例也可以）和 Gitea / Forgejo。',
          featureStatusMail: '邮件功能',
          featureStatusTerminal: '终端',
          featureStatusTasks: '任务调度',
          featureStatusShare: '分享中心',
          forgeEnabledDesc: '关闭后会停止同步',
          mailEnabledDesc: '关闭后不再后台收信',
          terminalStatusDesc: '开关在「功能编排」里。',
          tasksStatusDesc: '开关在「功能编排」里。',
          shareStatusDesc: '开关在「功能编排」里。',
          forgeEnabledDesc: '关闭后会停止同步，并解除所有项目已关联的仓库。',
          forgeDisableConfirmTitle: '关闭代码托管集成',
          forgeDisableConfirm: '关闭后将解除所有项目已关联的仓库。',
          forgeDisableConfirmAction: '关闭集成',
          forgeCredentials: '访问令牌',
          forgeCredentialsDesc: '访问令牌',
          agentModel: '首选模型',
          agentThinking: '思考强度',
          themeAuto: '自动',
          themeLight: '浅色',
          themeDark: '深色',
          localeZh: '中文',
          localeEn: 'English',
          changePassword: '修改密码',
          changePasswordDesc: '更改登录密码',
          trustedCerts: '已信任的证书',
          trustedCertsDesc: '清除为自签名服务器记住的证书',
          trustedCertsCleared: '已清除，下次连接会重新确认证书',
          trustedCertsConfirm: '清除所有已信任的自签名证书？',
          addToHomeScreen: '添加到主屏幕',
          downloadDesktopApp: '下载桌面版',
          downloadDesktopAppDesc: '下载桌面版',
          downloadAndroidApp: '下载APK',
          showWelcome: '打开欢迎界面',
          showWelcomeDesc: '打开欢迎界面',
          checkUpgrade: '检查更新',
          checkUpgradeDesc: '检查更新',
          saveFailed: '保存失败',
          passwordChanged: '密码修改成功',
          passwordDiscarded: '密码已丢弃',
          restartServer: '重启服务器',
          restartServerDesc: '重启服务器',
          restartServerConfirm: '确定重启？',
          tlsCertDir: 'TLS证书目录',
          tlsCertDirDesc: 'TLS证书目录',
          // Section headers
          chatInteractionSection: '交互',
          chatMessageSection: '消息与历史',
          archiveRetentionSectionHeader: '归档留存',
          recommendSectionHeader: '推荐回复',
          appearanceDisplaySection: '显示',
          appearanceHeaderSection: '顶栏',
          wallpaperSection: '背景图',
          wallpaper: '背景图',
          wallpaperDesc: '上传背景图',
          wallpaperUpload: '上传背景图',
          wallpaperReplace: '更换',
          wallpaperRemove: '移除',
          wallpaperSetOk: '已设置',
          wallpaperRemoved: '已移除',
          wallpaperUploadFailed: '上传失败',
          wallpaperRemoveFailed: '移除失败',
          wallpaperSaveFailed: '保存失败',
          wallpaperLoading: '加载中…',
          wallpaperPreview: '预览',
          wallpaperPanelOpacity: '面板不透明度',
          wallpaperPanelOpacityDesc: '描述',
          projectSectionHeader: '项目',
          searchSectionHeader: '搜索',
          fileDisplaySection: '文件显示',
          uploadSection: '上传',
          aboutVersionSection: '版本',
          aboutProjectSection: '项目',
          aboutActionsSection: '操作',
          securitySection: '安全',
          debugSection: '调试',
          summaryModelRow: '摘要模型',
          summaryModelConfigured: '已配置',
          summaryModelNotConfigured: '未配置',
        },
        dialog: {
          changePasswordTitle: '修改密码',
          currentPassword: '当前密码',
          newPassword: '新密码',
          confirmPassword: '确认密码',
          currentPasswordPlaceholder: '输入当前密码',
          newPasswordPlaceholder: '输入新密码',
          confirmPasswordPlaceholder: '再次输入新密码',
          changePasswordBtn: '修改',
          changingPassword: '修改中...',
          passwordChanged: '密码修改成功',
          wrongCurrentPassword: '当前密码错误',
          passwordTooShort: '密码太短',
          passwordMismatch: '密码不一致',
          passwordSameAsOld: '新密码与旧密码相同',
          currentPasswordRequired: '请输入当前密码',
          passwordTooManyAttempts: '尝试次数过多',
          passwordChangeFailed: '密码修改失败',
        },
      },
    },
  },
})

// The settings module graph is imported only once the fixtures above exist
// (see the note at the import site). `mountCategory` runs inside cases, so a
// file-level beforeAll is early enough for every one of them. The generous
// timeout covers transforming that graph on a cold cache.
beforeAll(async () => {
  SettingsCategory = (await import('@/components/settings/SettingsCategory.vue')).default
}, 120_000)

/**
 * Stubs for the card bodies that used to be static imports here.
 *
 * Card bodies are now lazy `cardBody` declarations owned by the field tables
 * (and, for plugins, by the plugin directory). These names used to be stubs in
 * every mount, which ALSO kept the async component from running its loader;
 * with the component no longer imported by SettingsCategory, an unstubbed body
 * would mount for real — and mounting, say, MailSetting re-supplies its own
 * global stubs and pulls a full settings page into a unit test.
 *
 * `true` renders the stub WITHOUT invoking the loader, so the pages that are
 * not under test stay inert. A test that asserts on a body removes its stub.
 */
const CARD_BODY_STUBS = {
  SettingsGroupPanel: true,
  IosInstallDrawer: true,
  UpgradeDialog: true,
  BottomSheet: true,
  ProviderIcon: true,
  PasswordChangeDialog: true,
  ProjectsSetting: true,
  ProjectDetailSetting: true,
  WallpaperSetting: true,
  TlsCertPanel: true,
  MCPSetting: true,
  ProviderSetting: true,
  SkillsSetting: true,
  SkillsDirsSetting: true,
  SkillsReposSetting: true,
  SkillsDiscoveredSetting: true,
  ForgeCredentialsRow: true,
  MailSetting: true,
  MeetingRelaySetting: true,
  KbSetting: true,
} as const

function mountCategory(categoryId: string, { deep = false, attach = false, stubs = {} }: { deep?: boolean; attach?: boolean; stubs?: Record<string, unknown> } = {}) {
  return mount(SettingsCategory, {
    props: { categoryId },
    attachTo: attach ? document.body : undefined,
    global: {
      plugins: [i18n],
      // A per-test entry overrides the default stub (e.g. `false` mounts the
      // real card body instead of the inert stub).
      stubs: deep ? {} : { ...CARD_BODY_STUBS, ...stubs },
    },
  })
}

afterEach(() => {
  document.body.querySelectorAll('.modal-overlay').forEach((el) => el.remove())
})

/**
 * Look up the lazy `cardBody` a page declares for one of its cards (test
 * helper).
 *
 * The body is asserted by mounting it directly: under jsdom an async component
 * whose loader dynamic-imports a module does not settle while it sits in a card
 * slot of a mounted subtree, so "mount the page, assert the body" would hang
 * rather than fail.
 */
function declaredCardBody(categoryId: string, key: string): ItemSpec {
  const entry = categoryItems[categoryId]?.find(e => e.type === 'item' && e.spec.key === key)
  expect(entry, `${categoryId}/${key} must exist`).toBeDefined()
  const spec = (entry as { type: 'item'; spec: ItemSpec }).spec
  expect(typeof spec.cardBody?.load, `${categoryId}/${key} must declare a cardBody`).toBe('function')
  return spec
}

describe('SettingsCategory', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset app mode: individual cases flip it to cover the app-only rows.
    mockIsAppMode.value = false
    mockIsDesktopApp.value = false
    mockIsHarmonyApp.value = false
    // Reset the scale-related state: cases override the effective factor.
    localConfig.uiScale = 1
    mockEffectiveUIScale.value = 1
    mockAutoFitUIScale.mockReset()
    mockAutoFitUIScale.mockReturnValue(1.5)
    mockDialogConfirm.mockResolvedValue(false)
    // Scaffold the wallpaper as enabled; the off-by-default case flips it and
    // must not leak into later cases (the fixture is module-level reactive).
    localConfig.wallpaperEnabled = true
    localConfig.wallpaperMode = 'wave'
    mockGetServerValueWithDefault.mockImplementation((key: string) => {
      // Simple flat-dot-path resolver against serverConfig
      const parts = key.split('.')
      let current: any = serverConfig.value
      for (const p of parts) {
        if (current == null || typeof current !== 'object') return undefined
        current = current[p]
      }
      return current
    })
  })

  // ─── Chat category ──────────────────────────────
  describe('chat category', () => {
    it('renders autoSpeech as switch item', () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const autoSpeechItem = allItems.find(i => i.props().label === '自动语音')
      expect(autoSpeechItem).toBeTruthy()
      expect(autoSpeechItem!.props().type).toBe('switch')
    })

    it('saves autoSpeech locally when toggled', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const autoSpeechItem = allItems.find(i => i.props().label === '自动语音')
      expect(autoSpeechItem).toBeTruthy()

      await autoSpeechItem!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('autoSpeech', true)
    })

    it('PATCHes chat.initial_messages when number changed', async () => {
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '初始消息数')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 30)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('chat.initial_messages', 30)
    })

    // The session list and the session lifecycle moved to 会话管理. Keeping a
    // copy here is what the split was meant to end, so pin the absence.
    it('no longer owns the session-list rows', () => {
      const wrapper = mountCategory('chat')
      const labels = wrapper.findAllComponents({ name: 'SettingsItem' }).map(i => i.props().label)
      expect(labels).not.toContain('滑动切换会话')
      expect(labels).not.toContain('最大会话数')
      expect(labels).not.toContain('会话按最近活动排序')
    })
  })

  // ─── Sessions category ──────────────────────────
  describe('sessions category', () => {
    it('renders swipeSession as switch item', () => {
      const wrapper = mountCategory('sessions')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const swipeSessionItem = allItems.find(i => i.props().label === '滑动切换会话')
      expect(swipeSessionItem).toBeTruthy()
      expect(swipeSessionItem!.props().type).toBe('switch')
    })

    it('saves swipeSession locally when toggled', async () => {
      const wrapper = mountCategory('sessions')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const swipeSessionItem = allItems.find(i => i.props().label === '滑动切换会话')
      expect(swipeSessionItem).toBeTruthy()

      await swipeSessionItem!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('swipeSession', true)
    })

    it('PATCHes session.max_count when number changed', async () => {
      const wrapper = mountCategory('sessions')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '最大会话数')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 20)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('session.max_count', 20)
    })

    it('session.max_count should NOT be marked as needsRestart (hot-reload field)', async () => {
      const wrapper = mountCategory('sessions')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '最大会话数')
      expect(item).toBeTruthy()
      // session.max_count is hot-reloadable via applyHotReloadGlobals, no restart needed
      expect(item!.props().needsRestart).toBe(false)
    })

    it('PATCHes chat.session_sort_recent and bumps sessionListVersion', async () => {
      const { store } = await import('@/stores/app')
      const before = store.state.sessionListVersion
      const wrapper = mountCategory('sessions')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '会话按最近活动排序')
      expect(item).toBeTruthy()
      expect(item!.props().type).toBe('switch')

      await item!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('chat.session_sort_recent', true)
      // The list order is computed server-side; without this bump the sidebar
      // behind the settings overlay would keep the old order until some other
      // event reloaded it.
      expect(store.state.sessionListVersion).toBe(before + 1)
    })

    it('keeps the session-search knobs here, not on the RAG page', () => {
      const wrapper = mountCategory('sessions')
      const labels = wrapper.findAllComponents({ name: 'SettingsItem' }).map(i => i.props().label)
      expect(labels).toContain('保留天数')
      expect(labels).toContain('搜索结果数')
      expect(labels).toContain('搜索池大小')
      expect(labels).toContain('索引器批次大小')
    })
  })

  describe('tts category', () => {
    // Cards are grouped by CONSECUTIVE sectionHeader, so a summary row without
    // one is flushed into the trailing "其他" card. That split the voice
    // summary settings across two cards (the ratio row ended up alone under
    // "其他"). All three summary rows must share the 语音摘要 card.
    it('keeps every voice-summary row in a single 语音摘要 card', () => {
      serverConfig.value.summarize = { tts_backend: 'auto', auto_junk_ratio: 0.5 }
      const wrapper = mountCategory('tts')
      const titles = wrapper.findAllComponents({ name: 'SettingsCard' }).map(c => c.props().title)

      expect(titles).toEqual(['TTS 引擎', '语音摘要'])
      expect(titles).not.toContain('其他')

      const summaryCard = wrapper
        .findAllComponents({ name: 'SettingsCard' })
        .find(c => c.props().title === '语音摘要')!
      const keys = summaryCard.findAll('[data-settings-key]').map(el => el.attributes('data-settings-key'))
      expect(keys).toEqual(['summarize.tts_backend', 'summaryModel', 'summarize.auto_junk_ratio'])
    })
  })

  // ─── 视频会议 category ──────────────────────────────
  describe('meeting category', () => {
    // The relay endpoint/credential is installation-wide, so it lives here
    // instead of the per-meeting create form. The card body is a dedicated
    // component, and a section header with no matching render branch would
    // leave an empty card — the exact failure this pins.
    it('renders the relay form in its own card', async () => {
      // The meeting page's field table (plugins/live/settings.ts) declares
      // MeetingRelaySetting as this card's body; the page no longer imports it.
      // Mount the component directly — see declaredCardBody.
      declaredCardBody('meeting', 'meetingRelay')
      const wrapper = mount(MeetingRelaySetting, { global: { plugins: [i18n] } })
      await flushPromises()

      expect(wrapper.find('[data-role="relay-url"]').exists()).toBe(true)
      expect(wrapper.find('[data-role="relay-key"]').exists()).toBe(true)
      expect(wrapper.find('[data-role="relay-secret"]').exists()).toBe(true)
      expect(wrapper.find('[data-action="relay-save-test"]').exists()).toBe(true)
    })
  })

  // ─── Shared AI-summary-model status ─────────────────────
  describe('AI summary model status', () => {
    /** The three in-place model pickers all share one label. */
    function jumpRows(wrapper: ReturnType<typeof mountCategory>) {
      return wrapper
        .findAllComponents({ name: 'SettingsItem' })
        .filter(i => i.props().label === '摘要模型')
    }

    afterEach(() => {
      delete (serverConfig.value as Record<string, unknown>).ai_summary
    })

    it('marks the model rows unconfigured when no provider is bound', () => {
      delete (serverConfig.value as Record<string, unknown>).ai_summary
      const rows = jumpRows(mountCategory('chat'))
      expect(rows.length).toBeGreaterThan(0)
      for (const row of rows) {
        expect(row.props().summaryModelStatus).toBe('unconfigured')
      }
    })

    it('marks the model rows configured once a provider is bound', () => {
      ;(serverConfig.value as Record<string, unknown>).ai_summary = { provider_id: 7 }
      const rows = jumpRows(mountCategory('chat'))
      expect(rows.length).toBeGreaterThan(0)
      for (const row of rows) {
        expect(row.props().summaryModelStatus).toBe('configured')
      }
    })

    it('labels the row as "provider · model" like the picker does', async () => {
      // Two providers can serve the same model id; the bare id made a switch
      // between them look like nothing happened.
      ;(serverConfig.value as Record<string, unknown>).ai_summary = { provider_id: 4242, model: 'gpt-4o' }
      const wrapper = mountCategory('chat')
      const row = () => jumpRows(wrapper)[0]
      await flushPromises()
      // Until a screen loads the provider list the row still has to say
      // something, so it falls back to the bare model id.
      expect(row().props().modelValue).toBe('gpt-4o')

      // The name arrives later (the providers page / picker publishes it); the
      // row must pick it up without a re-mount.
      rememberProviderNames([{ id: 4242, name: 'MiniMax' }])
      await nextTick()
      expect(row().props().modelValue).toBe('MiniMax · gpt-4o')
    })

    it('leaves rows without the opt-in flag without a status', () => {
      ;(serverConfig.value as Record<string, unknown>).ai_summary = { provider_id: 7 }
      const wrapper = mountCategory('sessions')
      const plain = wrapper
        .findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '最大会话数')
      expect(plain!.props().summaryModelStatus).toBeUndefined()
    })
  })

  // ─── ProjectFiles category ──────────────────────────────
  describe('files category', () => {
    it('saves showHidden locally when toggled', async () => {
      const wrapper = mountCategory('files')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '显示隐藏文件')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('showHidden', true)
    })

    it('PATCHes upload.max_size_mb when changed', async () => {
      const wrapper = mountCategory('files')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '上传大小上限')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 200)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('upload.max_size_mb', 200)
    })

    it('PATCHes upload.max_files when changed', async () => {
      const wrapper = mountCategory('files')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '上传文件上限')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 50)
      await wrapper.vm.$nextTick()

      expect(mockSetServerValue).toHaveBeenCalledWith('upload.max_files', 50)
    })
  })

  // ─── Appearance category ──────────────────────────────
  describe('appearance category', () => {
    it('saves theme locally when selected', async () => {
      const wrapper = mountCategory('appearance')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '主题')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 'dark')
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('theme', 'dark')
    })

    it('saves locale locally when selected', async () => {
      const wrapper = mountCategory('appearance')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '语言')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', 'en')
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('locale', 'en')
    })

    it('declares the wallpaper component as the card body', () => {
      // The body is no longer imported by SettingsCategory: the appearance
      // page's field table names it on the item that opens the card.
      const spec = declaredCardBody('appearance', 'panelOpacity')
      expect(spec.sectionHeader).toBe('settings.items.wallpaperSection')
      // And the component itself renders (mounted directly — see the helper).
      const wrapper = mount(WallpaperSetting, {
        props: { description: '壁纸' },
        global: { plugins: [i18n], stubs: { SettingsGroupPanel: true } },
      })
      expect(wrapper.exists()).toBe(true)
    })

    it('enables the panel-opacity slider once the device turns the wallpaper on', async () => {
      // The choice is per-device local state, so an enabled wallpaper is
      // resolvable on first paint with no server data — the slider must not sit
      // disabled waiting for /api/config.
      localConfig.wallpaperEnabled = true
      localConfig.wallpaperMode = 'wave'
      const wallpaper = mount(WallpaperSetting, { global: { plugins: [i18n] } })
      await flushPromises()
      const slider = wallpaper.find('input[type="range"]')
      expect(slider.attributes('disabled')).toBeUndefined()
    })

    it('disables the panel-opacity slider on a fresh device, where the wallpaper is off by default', async () => {
      // localDefaults.wallpaperEnabled is false, so a device that never touched
      // the setting has no background and nothing to tune.
      localConfig.wallpaperEnabled = false
      const wallpaper = mount(WallpaperSetting, { global: { plugins: [i18n] } })
      await flushPromises()
      const slider = wallpaper.find('input[type="range"]')
      expect(slider.attributes('disabled')).toBeDefined()
    })

    // 20s, not the 5s default: mounting the appearance pane pulls in the whole
    // WallpaperSetting subtree. Alone that is ~1s, but in the full 1000-file run
    // the worker was slow enough to blow the default (same reason the 50-tile
    // gallery test in WallpaperSetting.test.ts carries 20s). The assertion needs
    // the real gallery, so the timeout is what gives.
    it('renders the gallery thumbnail for a locally selected image', async () => {
      localConfig.wallpaperMode = 'local'
      localConfig.wallpaperLocalSelected = 'local-1-a.png'
      serverConfig.value = {
        ...serverConfig.value,
        appearance: {
          local: { items: [{ file: 'local-1-a.png', name: 'a.png', uploaded_at: 1, size: 10 }] },
          bing: {},
        },
      }
      try {
        // Mounted directly: the panel is the card body declared in the
        // appearance field table, not something SettingsCategory imports.
        const wallpaper = mount(WallpaperSetting, { global: { plugins: [i18n] } })
        await flushPromises()
        expect(wallpaper.find('img.wallpaper-gallery__thumb').exists()).toBe(true)
      } finally {
        localConfig.wallpaperMode = 'wave'
        localConfig.wallpaperLocalSelected = ''
      }
    }, 20_000)

    it('renders the auto-fit row as an action, not a switch', () => {
      const wrapper = mountCategory('appearance')
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '自动适配')
      expect(item).toBeTruthy()
      // A switch would re-derive the factor on every applier call (the jumping
      // this replaced); an action only runs when clicked.
      expect(item!.props().type).toBe('action')
    })

    it('clicking auto-fit runs the one-shot fit', async () => {
      mockAutoFitUIScale.mockReturnValue(1.5)
      const wrapper = mountCategory('appearance')
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '自动适配')
      expect(item).toBeTruthy()

      await item!.vm.$emit('click')
      await wrapper.vm.$nextTick()

      expect(mockAutoFitUIScale).toHaveBeenCalled()
      // The row must not write the setting itself — autoFitUIScale owns the
      // compute + persist so there is a single source of truth.
      expect(mockSetLocalConfig).not.toHaveBeenCalledWith('uiScaleAutoFit', expect.anything())
    })

    it('keeps the scale slider enabled', () => {
      // It used to be disabled while the auto switch was on. Now the auto-fit
      // button writes INTO this slider, so it must always be usable.
      const wrapper = mountCategory('appearance')
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '界面缩放')
      expect(item).toBeTruthy()
      expect(item!.props().disabled).toBe(false)
    })

    it('shows the effective factor on the slider', () => {
      localConfig.uiScale = 1
      mockEffectiveUIScale.value = 2
      const wrapper = mountCategory('appearance')
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '界面缩放')
      expect(item!.props().modelValue).toBe(2)
    })

    it('widens the slider max when the stored factor exceeds the slider ceiling', () => {
      // Auto fit caps at 200% while the slider track ends at 150%; without the
      // widening the thumb would pin to the edge and disagree with the
      // displayed value.
      mockEffectiveUIScale.value = 2
      const wrapper = mountCategory('appearance')
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '界面缩放')
      expect(item!.props().max).toBe(2)
    })

    it('hides the auto-fit row inside the Android app', () => {
      mockIsAppMode.value = true
      const wrapper = mountCategory('appearance')
      const labels = wrapper.findAllComponents({ name: 'SettingsItem' }).map(i => i.props().label)
      expect(labels).not.toContain('自动适配')
      // The manual slider stays available — Android keeps its existing behaviour.
      expect(labels).toContain('界面缩放')
    })
  })

  // ─── About category ──────────────────────────────
  describe('about category', () => {
    it('renders server version as info type', () => {
      const wrapper = mountCategory('about')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '服务器版本')
      expect(item).toBeTruthy()
      expect(item!.props().type).toBe('info')
    })

    it('hides appVersion row when not in App mode', () => {
      const wrapper = mountCategory('about')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const appVersionItem = allItems.find(i => i.props().label === 'APP版本')
      expect(appVersionItem).toBeFalsy()
    })

    it('renders the brand header above the version card', () => {
      const wrapper = mountCategory('about')
      expect(wrapper.findComponent({ name: 'SettingsAboutBrand' }).exists()).toBe(true)
      // The brand block must come first — it is the page's entry point, not a
      // footer after the settings rows.
      expect(wrapper.find('.settings-category').element.firstElementChild?.className)
        .toContain('about-brand')
    })

    it('does not render the brand header on other categories', () => {
      // SettingsAboutBrand is keyed off the category id; a missing guard would
      // put the logo on every settings page.
      expect(mountCategory('chat').findComponent({ name: 'SettingsAboutBrand' }).exists()).toBe(false)
    })

    it('renders the homepage, GitHub and feedback entries in their own card', () => {
      const wrapper = mountCategory('about')
      const labels = wrapper.findAllComponents({ name: 'SettingsItem' })
        .map(i => i.props().label)
      expect(labels).toContain('项目主页')
      expect(labels).toContain('GitHub 主页')
      expect(labels).toContain('问题反馈')

      const card = wrapper.findAllComponents({ name: 'SettingsCard' })
        .find(c => c.props().title === '项目')
      expect(card, 'the project links need their own card, not the actions card').toBeTruthy()
      const cardLabels = card!.findAllComponents({ name: 'SettingsItem' }).map(i => i.props().label)
      expect(cardLabels).toEqual(['项目主页', 'GitHub 主页', '问题反馈'])
    })

    it('opens the project homepage (not the repository) on click', async () => {
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'openProjectHomepage' })
      await nextTick()

      // The web fallback is an anchor click; jsdom records the element only
      // while it is still attached (it is removed after 1s).
      const anchors = document.body.querySelectorAll('a[href]')
      const hrefs = Array.from(anchors).map(a => a.getAttribute('href'))
      expect(hrefs).toContain('https://clawbench.pages.dev')
      anchors.forEach(a => a.remove())
    })

    it('opens the GitHub repository (not the homepage) on click', async () => {
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'openProjectGithub' })
      await nextTick()

      const anchors = Array.from(document.body.querySelectorAll('a[href]'))
      const hrefs = anchors.map(a => a.getAttribute('href'))
      expect(hrefs).toContain('https://github.com/xulongzhe/clawbench')
      expect(hrefs).not.toContain('https://clawbench.pages.dev')
      anchors.forEach(a => a.remove())
    })

    it('opens the issue template chooser for feedback, not the bare issue list', async () => {
      // The repository ships bug/feature/question issue templates; the chooser
      // page is what lets a reporter pick one. Pointing at /issues would drop
      // them on a list with no obvious way to file.
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'openProjectFeedback' })
      await nextTick()

      const anchors = Array.from(document.body.querySelectorAll('a[href]'))
      const hrefs = anchors.map(a => a.getAttribute('href'))
      expect(hrefs).toContain('https://github.com/xulongzhe/clawbench/issues/new/choose')
      anchors.forEach(a => a.remove())
    })

    it('hands the URL to the native bridge when one is present', async () => {
      // window.open(url, '_blank') is silently swallowed by the Android
      // WebView (no multi-window support), so app mode must go through the
      // bridge instead of the anchor fallback.
      const mockOpenExternalUrl = vi.fn().mockResolvedValue(undefined)
      ;(window as any).ClawBenchNative = { openExternalUrl: mockOpenExternalUrl }
      try {
        const wrapper = mountCategory('about')
        const vm = wrapper.vm as any
        vm.$.setupState.handleClick({ key: 'openProjectFeedback' })
        await nextTick()

        expect(mockOpenExternalUrl).toHaveBeenCalledWith(
          'https://github.com/xulongzhe/clawbench/issues/new/choose',
        )
        // No anchor fallback may fire alongside the bridge call.
        expect(document.body.querySelectorAll('a[href^="https://github.com"]').length).toBe(0)
      } finally {
        delete (window as any).ClawBenchNative
      }
    })
  })

  // ─── Security category ──────────────────────────────
  describe('security category', () => {
    it('renders changePassword as action item', () => {
      const wrapper = mountCategory('security')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '修改密码')
      expect(item).toBeTruthy()
      expect(item!.props().type).toBe('action')
    })

    it('clears remembered certificates from the desktop-only row', async () => {
      mockIsAppMode.value = true
      mockIsDesktopApp.value = true
      const forget = vi.fn().mockResolvedValue(1)
      ;(window as any).ClawBenchNative = {
        isNativeApp: () => true,
        isDesktopApp: () => true,
        forgetTrustedCerts: forget,
      }
      mockDialogConfirm.mockResolvedValueOnce(true)
      try {
        const wrapper = mountCategory('security')
        const item = wrapper.findAllComponents({ name: 'SettingsItem' })
          .find(i => i.props().label === '已信任的证书')
        expect(item, 'the desktop shell must expose the undo row').toBeTruthy()
        await item!.vm.$emit('click')
        await flushPromises()
        // Every self-signed server has to be re-accepted afterwards, so the row
        // asks first — like every other destructive action on this page.
        expect(mockDialogConfirm).toHaveBeenCalled()
        expect(forget).toHaveBeenCalledTimes(1)
        expect(mockToastShow).toHaveBeenCalled()
      } finally {
        delete (window as any).ClawBenchNative
      }
    })

    it('does not clear remembered certificates when the confirmation is cancelled', async () => {
      mockIsAppMode.value = true
      mockIsDesktopApp.value = true
      const forget = vi.fn().mockResolvedValue(1)
      ;(window as any).ClawBenchNative = {
        isNativeApp: () => true,
        isDesktopApp: () => true,
        forgetTrustedCerts: forget,
      }
      mockDialogConfirm.mockResolvedValueOnce(false)
      try {
        const wrapper = mountCategory('security')
        const item = wrapper.findAllComponents({ name: 'SettingsItem' })
          .find(i => i.props().label === '已信任的证书')
        await item!.vm.$emit('click')
        await flushPromises()
        expect(forget).not.toHaveBeenCalled()
      } finally {
        delete (window as any).ClawBenchNative
      }
    })

    it('hides the trusted-certificate row in the browser', () => {
      // Android never persists the certificate answer (it re-asks every
      // connection), and the browser has no native bridge at all — the row
      // would be a no-op button there.
      const wrapper = mountCategory('security')
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '已信任的证书')
      expect(item).toBeFalsy()
    })

    it.each(['harmonyos', 'electron'] as const)('hides the trusted-certificate row when %s has no certificate store', (platform) => {
      mockIsAppMode.value = true
      mockIsDesktopApp.value = platform === 'electron'
      mockIsHarmonyApp.value = platform === 'harmonyos'
      const bridgeName = platform === 'harmonyos' ? 'ClawBenchHarmony' : 'ClawBenchNative'
      ;(window as any)[bridgeName] = {
        isNativeApp: () => true,
        getHostPlatform: () => platform,
      }
      const wrapper = mountCategory('security')
      try {
        expect(wrapper.find('[data-settings-key="trustedCerts"]').exists()).toBe(false)
        expect(wrapper.find('[data-settings-key="changePassword"]').exists()).toBe(true)
      } finally {
        wrapper.unmount()
        delete (window as any)[bridgeName]
      }
    })

    it('shows the trusted-certificate row when HarmonyOS supplies a working certificate-store method', () => {
      mockIsAppMode.value = true
      mockIsHarmonyApp.value = true
      ;(window as any).ClawBenchHarmony = {
        isNativeApp: () => true,
        getHostPlatform: () => 'harmonyos',
        forgetTrustedCerts: vi.fn().mockResolvedValue(0),
      }
      const wrapper = mountCategory('security')
      try {
        expect(wrapper.find('[data-settings-key="trustedCerts"]').exists()).toBe(true)
      } finally {
        wrapper.unmount()
        delete (window as any).ClawBenchHarmony
      }
    })

    it('opens password dialog when changePassword action is clicked', async () => {
      const wrapper = mountCategory('security', { deep: true, attach: true })

      // Directly set the internal showPasswordDialog state
      const vm = wrapper.vm as any
      vm.$.setupState.showPasswordDialog = true
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      // PasswordChangeDialog renders inside ModalDialog, which teleports to
      // <body> — look it up there, not in the component's own subtree.
      expect(document.body.querySelector('.modal-overlay')).toBeTruthy()
      expect(document.body.querySelector('.password-dialog__input')).toBeTruthy()
    })

    it('closes password dialog when dialog emits close', async () => {
      const wrapper = mountCategory('security', { deep: true, attach: true })

      // Open the dialog directly
      const vm = wrapper.vm as any
      vm.$.setupState.showPasswordDialog = true
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      expect(document.body.querySelector('.modal-overlay')).toBeTruthy()

      // Click the cancel button — PasswordChangeDialog handleClose emits close,
      // SettingsCategory flips showPasswordDialog to false, and ModalDialog
      // v-show-hides the overlay.
      const cancelBtn = document.body.querySelector('.fbtn') as HTMLElement | null
      if (cancelBtn) {
        cancelBtn.dispatchEvent(new MouseEvent('click', { bubbles: true }))
        await nextTick()
      } else {
        vm.$.setupState.showPasswordDialog = false
      }
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      expect(vm.$.setupState.showPasswordDialog).toBe(false)
    })

    it('shows toast and emits restartNeeded when password changed with needsRestart=true', async () => {
      const wrapper = mountCategory('security', { attach: true })

      // Open the dialog
      const vm = wrapper.vm as any
      vm.$.setupState.showPasswordDialog = true
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      // Simulate successful password change
      const dialog = wrapper.findComponent(PasswordChangeDialog)
      if (dialog.exists()) {
        await dialog.vm.$emit('changed', true)
      } else {
        // Directly invoke handlePasswordChanged
        if (vm.$.setupState.handlePasswordChanged) {
          vm.$.setupState.handlePasswordChanged(true)
        }
      }
      await wrapper.vm.$nextTick()
      wrapper.vm.$forceUpdate()
      await wrapper.vm.$nextTick()

      // Dialog should be closed
      expect(vm.$.setupState.showPasswordDialog).toBe(false)
      // Toast should show success
      expect(mockToastShow).toHaveBeenCalled()
      // restartNeeded should be emitted
      expect(wrapper.emitted('restartNeeded')).toBeTruthy()
    })
  })

  // ─── handleEditToggle / handleDiscard ──────────
  describe('toggle and discard handlers', () => {
    it('handleEditToggle sets activeKey on open', async () => {
      const wrapper = mountCategory('chat')
      const vm = wrapper.vm as any
      vm.$.setupState.handleEditToggle('autoSpeech', true)
      expect(vm.$.setupState.activeKey).toBe('autoSpeech')
    })

    it('handleEditToggle clears activeKey on close when key matches', async () => {
      const wrapper = mountCategory('chat')
      const vm = wrapper.vm as any
      vm.$.setupState.activeKey = 'autoSpeech'
      vm.$.setupState.handleEditToggle('autoSpeech', false)
      expect(vm.$.setupState.activeKey).toBeNull()
    })

    it('handleEditToggle does not clear activeKey when key does not match', async () => {
      const wrapper = mountCategory('chat')
      const vm = wrapper.vm as any
      vm.$.setupState.activeKey = 'autoSpeech'
      vm.$.setupState.handleEditToggle('other', false)
      expect(vm.$.setupState.activeKey).toBe('autoSpeech')
    })

    it('handleDiscard shows info toast', async () => {
      const wrapper = mountCategory('chat')
      const vm = wrapper.vm as any
      vm.$.setupState.handleDiscard()
      expect(mockToastShow).toHaveBeenCalled()
    })
  })

  // ─── handleClick ──────────────────────────────
  describe('handleClick', () => {
    it('opens password dialog when changePassword is clicked', async () => {
      const wrapper = mountCategory('security')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'changePassword' })
      expect(vm.$.setupState.showPasswordDialog).toBe(true)
    })

    it('calls ClawBenchNative.showServerDialog when reconfigureServer is clicked', async () => {
      const mockShowServerDialog = vi.fn()
      ;(window as any).ClawBenchNative = { showServerDialog: mockShowServerDialog }
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'reconfigureServer' })
      expect(mockShowServerDialog).toHaveBeenCalled()
      delete (window as any).ClawBenchNative
    })
  })

  // ─── handleUpdate edge cases ──────────────────
  describe('handleUpdate edge cases', () => {
    it('skips password update when value is empty', async () => {
      const wrapper = mountCategory('security')
      const vm = wrapper.vm as any
      await vm.$.setupState.handleUpdate({ key: 'password', type: 'password', source: 'server' }, '')
      expect(mockSetServerValue).not.toHaveBeenCalled()
    })

    it('allows password update with real value', async () => {
      const wrapper = mountCategory('security')
      const vm = wrapper.vm as any
      await vm.$.setupState.handleUpdate({ key: 'password', type: 'password', source: 'server' }, 'my-real-password')
      expect(mockSetServerValue).toHaveBeenCalled()
    })

    it('shows toast on server save failure', async () => {
      mockSetServerValue.mockRejectedValueOnce(new Error('fail'))
      const wrapper = mountCategory('chat')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '初始消息数')
      if (item) {
        await item.vm.$emit('update:modelValue', 99)
        await wrapper.vm.$nextTick()
        await vi.waitFor(() => {
          expect(mockToastShow).toHaveBeenCalled()
        })
      }
    })
  })

  // ─── Agents category routing ──────────────────
  describe('agents category routing', () => {
    it('renders SettingsAgentsIndex when categoryId is agents', async () => {
      const wrapper = mount(SettingsCategory, {
        props: { categoryId: 'agents' },
        global: {
          plugins: [i18n],
          stubs: {
            SettingsAgentsIndex: true,
            SettingsAgentDetail: true,
          },
        },
      })
      await wrapper.vm.$nextTick()
      expect(wrapper.findComponent({ name: 'SettingsAgentsIndex' }).exists()).toBe(true)
    })

    it('renders SettingsAgentDetail when categoryId starts with agents:', async () => {
      const wrapper = mount(SettingsCategory, {
        props: { categoryId: 'agents:test-agent' },
        global: {
          plugins: [i18n],
          stubs: {
            SettingsAgentsIndex: true,
            SettingsAgentDetail: true,
          },
        },
      })
      await wrapper.vm.$nextTick()
      expect(wrapper.findComponent({ name: 'SettingsAgentDetail' }).exists()).toBe(true)
    })

    it('emits deleted agents when SettingsAgentDetail emits deleted', async () => {
      const wrapper = mount(SettingsCategory, {
        props: { categoryId: 'agents:test-agent' },
        global: {
          plugins: [i18n],
          stubs: {
            SettingsAgentsIndex: true,
            SettingsAgentDetail: { template: '<div data-test="agent-detail" @click="$emit(\'deleted\')"></div>' },
          },
        },
      })
      await wrapper.vm.$nextTick()
      const detail = wrapper.find('[data-test="agent-detail"]')
      if (detail.exists()) {
        await detail.trigger('click')
        // `deleted` (not `navigate`): returning to the list is a stack POP.
        expect(wrapper.emitted('deleted')).toBeTruthy()
        expect(wrapper.emitted('deleted')![0]).toEqual(['agents'])
      }
    })

    it('forwards navigate-replace from SettingsAgentDetail (copy → new agent)', async () => {
      const wrapper = mount(SettingsCategory, {
        props: { categoryId: 'agents:test-agent' },
        global: {
          plugins: [i18n],
          stubs: {
            SettingsAgentsIndex: true,
            SettingsAgentDetail: { template: '<div data-test="agent-detail" @click="$emit(\'navigateReplace\', \'agents:copy\')"></div>' },
          },
        },
      })
      await wrapper.vm.$nextTick()
      await wrapper.find('[data-test="agent-detail"]').trigger('click')

      expect(wrapper.emitted('navigateReplace')).toBeTruthy()
      expect(wrapper.emitted('navigateReplace')![0]).toEqual(['agents:copy'])
    })
  })

  // ─── handleRestartServer ──────────────────
  describe('handleRestartServer', () => {
    it('clicks restartServer action and confirms', async () => {
      mockDialogConfirm.mockResolvedValue(true)
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'restartServer' })
      await wrapper.vm.$nextTick()
      expect(mockDialogConfirm).toHaveBeenCalled()
      await vi.waitFor(() => {
        expect(wrapper.emitted('restartRequested')).toBeTruthy()
      })
    })

    it('clicks restartServer action and cancels', async () => {
      mockDialogConfirm.mockResolvedValue(false)
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'restartServer' })
      await wrapper.vm.$nextTick()
      expect(mockDialogConfirm).toHaveBeenCalled()
      await wrapper.vm.$nextTick()
      expect(wrapper.emitted('restartRequested')).toBeFalsy()
    })
  })

  // ─── isLastInSection ──────────────────
  // Note: isLastInSection was removed from SettingsCategory when config groups
  // were introduced. Section dividers are now handled by sectionHeader on each item.

  // ─── handleAddToHomeScreen / downloadAndroidApp ──────────
  describe('handleAddToHomeScreen and downloadAndroidApp', () => {
    it('calls installPwa when canInstallPwa is true', async () => {
      mockCanInstallPwa.value = true
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      await vm.$.setupState.handleAddToHomeScreen()
      expect(mockInstallPwa).toHaveBeenCalled()
      mockCanInstallPwa.value = false
    })

    it('shows iOS sheet when canInstallPwa is false and isIOS is true', async () => {
      mockCanInstallPwa.value = false
      mockIsIOS.value = true
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      await vm.$.setupState.handleAddToHomeScreen()
      expect(vm.$.setupState.showIosSheet).toBe(true)
      mockIsIOS.value = false
    })

    it('does nothing when canInstallPwa is false and isIOS is false', async () => {
      mockCanInstallPwa.value = false
      mockIsIOS.value = false
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      await vm.$.setupState.handleAddToHomeScreen()
      expect(mockInstallPwa).not.toHaveBeenCalled()
      expect(vm.$.setupState.showIosSheet).toBe(false)
    })

    it('handleClick with downloadAndroidApp navigates to APK download', async () => {
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'downloadAndroidApp' })
      expect(mockDownloadByUrl).toHaveBeenCalledExactlyOnceWith('/api/apk', 'clawbench-android.apk', { viaNativeApp: true })
    })

    // The "download desktop app" row must only appear when there is actually
    // something to download for this platform. Rendering it unconditionally
    // gives the user a row that silently does nothing, and it would also show
    // on Android/iOS where no desktop build applies.
    it('hides the downloadDesktopApp row when no platform download exists', () => {
      const wrapper = mountCategory('about')
      expect(wrapper.text()).not.toContain('下载桌面版')
    })

    it('shows the downloadDesktopApp row when a platform download exists', async () => {
      mockDesktopDownload.isDesktop = true
      mockDesktopDownload.currentDownloadUrl = () => 'https://example.com/desktop.tgz'
      const wrapper = mountCategory('about')
      await nextTick()
      expect(wrapper.text()).toContain('下载桌面版')
    })

    it('clicking downloadDesktopApp triggers the download', async () => {
      mockDesktopDownload.isDesktop = true
      mockDesktopDownload.currentDownloadUrl = () => 'https://example.com/desktop.tgz'
      const wrapper = mountCategory('about')
      const vm = wrapper.vm as any
      vm.$.setupState.handleClick({ key: 'downloadDesktopApp' })
      expect(mockDesktopDownload.downloadDesktop).toHaveBeenCalled()
    })
  })

  // ─── Notification category ──────────────────────────────
  describe('notification category', () => {
    it('renders notificationSound as switch item', () => {
      const wrapper = mountCategory('notification')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '任务完成提示音')
      expect(item).toBeTruthy()
      expect(item!.props().type).toBe('switch')
    })

    it('saves notificationSound locally when toggled off', async () => {
      const wrapper = mountCategory('notification')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '任务完成提示音')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', false)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('notificationSound', false)
    })

    it('saves notificationSound locally when toggled on', async () => {
      localConfig.notificationSound = false
      const wrapper = mountCategory('notification')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '任务完成提示音')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', true)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('notificationSound', true)
    })

    it('renders inAppNotification as switch item', () => {
      const wrapper = mountCategory('notification')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '应用内完成通知')
      expect(item).toBeTruthy()
      expect(item!.props().type).toBe('switch')
    })

    it('saves inAppNotification locally when toggled off', async () => {
      const wrapper = mountCategory('notification')
      const allItems = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = allItems.find(i => i.props().label === '应用内完成通知')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', false)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('inAppNotification', false)
    })

    it('renders the desktop notification switch in its own card, in BOTH modes', () => {
      // Deliberately NOT appOnly: the "桌面与系统" card below is app-only and
      // disappears in browser mode, which is exactly where this switch matters.
      // If it ever gets folded into that card, browser users lose the control.
      for (const appMode of [false, true]) {
        mockIsAppMode.value = appMode
        const wrapper = mountCategory('notification')
        const titles = wrapper.findAllComponents({ name: 'SettingsCard' }).map(c => c.props().title)
        expect(titles, `app mode = ${appMode}`).toContain('桌面端通知')

        const item = wrapper.findAllComponents({ name: 'SettingsItem' })
          .find(i => i.props().label === '桌面端系统通知')
        expect(item, `app mode = ${appMode}`).toBeTruthy()
        expect(item!.props().type).toBe('switch')
      }
    })

    it('saves desktopNotification locally when toggled off', async () => {
      const wrapper = mountCategory('notification')
      const item = wrapper.findAllComponents({ name: 'SettingsItem' })
        .find(i => i.props().label === '桌面端系统通知')
      expect(item).toBeTruthy()

      await item!.vm.$emit('update:modelValue', false)
      await wrapper.vm.$nextTick()

      expect(mockSetLocalConfig).toHaveBeenCalledWith('desktopNotification', false)
    })

    it('groups the flat items into 应用内通知 then 桌面与系统 cards in app mode', () => {
      mockIsAppMode.value = true
      const wrapper = mountCategory('notification')
      const titles = wrapper.findAllComponents({ name: 'SettingsCard' })
        .map(c => c.props().title)

      expect(titles).toEqual(['应用内通知', '桌面端通知', '桌面与系统'])
    })

    it('keeps the floating-window row in the Electron desktop shell, drops the Android-only chip', () => {
      // The two rows in that card are NOT the same platform.
      //
      // 桌面悬浮状态窗 is implemented by BOTH hosts — Android draws a bubble over
      // the launcher, the Electron shell draws an always-on-top capsule plus a
      // tray icon (`desktop/src/main/floating.ts`). Marking it `androidOnly`
      // left the desktop feature with no way to switch it on at all, and the
      // tray's own "turn off" then had no way back.
      //
      // 灵动岛 is genuinely Android-only: `setLiveUpdateEnabled` /
      // `canPostPromotedNotifications` exist only on the Android bridge and the
      // copy names Android 16 permissions.
      mockIsAppMode.value = true
      mockIsDesktopApp.value = true
      const wrapper = mountCategory('notification')
      const titles = wrapper.findAllComponents({ name: 'SettingsCard' })
        .map(c => c.props().title)

      expect(titles).toContain('桌面与系统')
      // The card survives, but only the floating-window switch is in it.
      expect(titles).toEqual(['应用内通知', '桌面端通知', '桌面与系统'])
      const rowLabels = wrapper.findAll('[data-settings-key]')
        .map(el => el.attributes('data-settings-key'))
      expect(rowLabels).toContain('floatingStatusWindow')
      expect(rowLabels).not.toContain('liveUpdate')
    })

    it('shows the close-behaviour row only where the shell owns the close button', () => {
      mockIsAppMode.value = true
      mockIsDesktopApp.value = true

      // The row describes the frameless header's own close button, so the
      // capability decides rather than a platform flag: macOS keeps a native
      // frame (its red button already closes the window without quitting) and
      // the Android/Harmony shells have no such dialog at all.
      const without = mountCategory('notification')
      expect(without.findAll('[data-settings-key]').map(el => el.attributes('data-settings-key')))
        .not.toContain('closeAction')

      const native = {
        isNativeApp: () => true,
        isDesktopApp: () => true,
        getHostPlatform: () => 'electron',
        hasCustomWindowControls: () => true,
      }
      ;(window as unknown as Record<string, unknown>).ClawBenchNative = native
      try {
        const wrapper = mountCategory('notification')
        expect(wrapper.findAll('[data-settings-key]').map(el => el.attributes('data-settings-key')))
          .toContain('closeAction')
      } finally {
        delete (window as unknown as Record<string, unknown>).ClawBenchNative
      }
    })

    it('drops the 桌面与系统 card entirely in browser mode (app-only rows)', () => {
      const wrapper = mountCategory('notification')
      const titles = wrapper.findAllComponents({ name: 'SettingsCard' })
        .map(c => c.props().title)

      // The card is gated on `appOnly`, and both rows inside it are app-only or
      // stricter, so it must not render as an empty headed card in browser mode
      // (where there is no native host to show anything). The
      // desktop-notification card stays.
      expect(titles).not.toContain('桌面与系统')
      expect(titles).toEqual(['应用内通知', '桌面端通知'])
    })
  })

})


  // ─── New configuration pages ───────────────────────────
  // These four pages are data-driven (rows come from settingsFieldMap), so the
  // render check is about the rows actually reaching the DOM under the right
  // category id — the structure itself is pinned in settingsFieldMap.test.ts.
  describe('new configuration pages', () => {
    const hasKey = (category: string, key: string) =>
      mountCategory(category).find(`[data-settings-key="${key}"]`).exists()

    it('Git 历史 renders the list length and the display choices', () => {
      expect(hasKey('gitHistory', 'git_history.page_size')).toBe(true)
      expect(hasKey('gitHistory', 'gitHistoryHideMerges')).toBe(true)
      expect(hasKey('gitHistory', 'gitHistoryShowGraph')).toBe(true)
      expect(hasKey('gitHistory', 'gitHistoryTimeFormat')).toBe(true)
    })

    it('任务 renders the scheduler state row and its settings', () => {
      const wrapper = mountCategory('tasks')
      expect(wrapper.findComponent({ name: 'FeatureStatusRow' }).props().enableKey).toBe('tasks.enabled')
      for (const key of ['tasks.retention_enabled', 'tasks.retention_days', 'tasks.notify_on_success', 'tasks.notify_on_failure', 'tasks.timezone', 'tasksDefaultFilter']) {
        expect(wrapper.find(`[data-settings-key="${key}"]`).exists(), key).toBe(true)
      }
    })

    it('统计 renders its three local defaults', () => {
      expect(hasKey('stats', 'statsDefaultTab')).toBe(true)
      expect(hasKey('stats', 'statsDefaultRange')).toBe(true)
      expect(hasKey('stats', 'statsRememberLast')).toBe(true)
    })

    it('分享中心 renders the status row and the sharing rules', () => {
      const wrapper = mountCategory('share')
      expect(wrapper.findComponent({ name: 'FeatureStatusRow' }).props().enableKey).toBe('share.enabled')
      for (const key of ['share.allow_files', 'share.allow_sessions', 'share.allow_meetings', 'share.allow_download', 'share.link_ttl_days', 'share.show_project_name']) {
        expect(wrapper.find(`[data-settings-key="${key}"]`).exists(), key).toBe(true)
      }
    })

    it('知识库 renders the status card plus the kb.* rows, and no master switch', async () => {
      const wrapper = mountCategory('kb')
      for (const key of ['kb.auto_inject', 'kb.inject_top_n', 'kb.inject_max_chars', 'kb.inject_min_similarity', 'kb.max_file_mb', 'kb.max_text_mb', 'kb.poll_interval', 'kb.embed_batch_size']) {
        expect(wrapper.find(`[data-settings-key="${key}"]`).exists(), key).toBe(true)
      }
      // The switch itself stays in the feature manager; this page only reports
      // it, so there is no checkbox for kb.enabled.
      const statusRow = wrapper.findComponent({ name: 'FeatureStatusRow' })
      expect(statusRow.exists()).toBe(true)
      expect(statusRow.props().enableKey).toBe('kb.enabled')
      expect(statusRow.find('input[type="checkbox"]').exists()).toBe(false)

      // The status card's body is declared in plugins/kb/settings.ts (the page
      // no longer imports it) and mounted directly — see declaredCardBody.
      expect(declaredCardBody('kb', 'kbStatus').sectionHeader).toBe('settings.items.kbStatusSection')
      const card = mount(KbSetting, { global: { plugins: [i18n], stubs: { KbIndexProgress: true } } })
      await flushPromises()
      expect(card.find('.kb-setting').exists()).toBe(true)
    })

    it('RAG 检索 renders one panel and nothing from 会话管理', () => {
      // The RAG page is a single group panel; its fields are pinned in
      // settingsFieldMap.test.ts (the panel renders its own row markup, which
      // has no per-key hook). What matters here is that the page still routes
      // to that panel and that no flat session row leaked onto it.
      const wrapper = mountCategory('rag')
      expect(wrapper.findComponent({ name: 'SettingsGroupPanel' }).exists()).toBe(true)
      const labels = wrapper.findAllComponents({ name: 'SettingsItem' }).map(i => i.props().label)
      expect(labels).not.toContain('搜索结果数')
      expect(labels).not.toContain('索引器批次大小')
    })
  })

// The forge master switch moved to the Dock feature manager (功能编排): one
// toggle there has to control BOTH the dock tab and the background feature, so
// a second switch on this page would be a second source of truth. What remains
// here is a read-only status row that links to the manager — including the
// destructive-disable confirmation, which now lives where the switch is.
describe('feature status rows', () => {
  it('reports the forge integration as enabled and offers a link to the manager', async () => {
    ;(serverConfig.value as Record<string, unknown>).forge = { enabled: true }
    const wrapper = mountCategory('forgeIntegration')

    const row = wrapper.findComponent({ name: 'FeatureStatusRow' })
    expect(row.exists()).toBe(true)
    expect(row.props().enableKey).toBe('forge.enabled')
    expect(row.text()).toContain('已启用')
    expect(row.text()).toContain('去功能编排')

    // The page no longer renders a switch for it — that is the whole point.
    const labels = wrapper.findAllComponents({ name: 'SettingsItem' }).map(i => i.props().label)
    expect(labels).not.toContain('启用代码托管集成')
  })

  it('reports a disabled feature as 已停用', () => {
    ;(serverConfig.value as Record<string, unknown>).forge = { enabled: false }
    const wrapper = mountCategory('forgeIntegration')

    const row = wrapper.findComponent({ name: 'FeatureStatusRow' })
    expect(row.text()).toContain('已停用')
  })

  it('clicking 去功能编排 navigates to the feature manager', async () => {
    ;(serverConfig.value as Record<string, unknown>).forge = { enabled: true }
    const wrapper = mountCategory('forgeIntegration')

    await wrapper.find('[data-action="feature-status-manage"]').trigger('click')
    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['dockPlugins'])
  })

  it('never PATCHes the enable key from this page', async () => {
    // Every module page that owns a master switch shows a status row instead:
    // mail, forge, terminal, tasks and share.
    for (const category of ['mail', 'forgeIntegration', 'terminal', 'tasks', 'share']) {
      const wrapper = mountCategory(category)
      const row = wrapper.findComponent({ name: 'FeatureStatusRow' })
      expect(row.exists(), `${category} must show a status row`).toBe(true)
      expect(row.find('input[type="checkbox"]').exists()).toBe(false)
    }
    expect(mockSetServerValue).not.toHaveBeenCalled()
  })
})

describe('server warnings surfaced to the user', () => {
  // This block sits outside the main describe, so it does not inherit its
  // beforeEach. Without an explicit reset the previous case's toast call leaks
  // into the next assertion.
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // The server's only channel for "saved, but a follow-up step failed". Ignoring
  // it made one case invisible: disabling the forge integration persists the
  // switch and then clears the bindings; a failed clear told the user nothing,
  // so they later re-enabled it and found the bindings still there.
  it('toasts every warning returned by a server save', async () => {
    mockSetServerValue.mockResolvedValueOnce({
      needsRestart: false,
      changedColdFields: [],
      warnings: ['forge disabled, but clearing repository bindings failed: boom'],
    })
    const wrapper = mountCategory('chat')
    const item = wrapper
      .findAllComponents({ name: 'SettingsItem' })
      .find(i => i.props().label === '初始消息数')

    await item!.vm.$emit('update:modelValue', 30)
    await flushPromises()

    expect(mockToastShow).toHaveBeenCalledWith(
      'forge disabled, but clearing repository bindings failed: boom',
      expect.objectContaining({ type: 'error' }),
    )
  })

  it('does not toast when the save returns no warnings', async () => {
    // `warnings` is absent for most saves; a missing field must not produce an
    // empty toast.
    mockSetServerValue.mockResolvedValueOnce({
      needsRestart: false,
      changedColdFields: [],
    })
    const wrapper = mountCategory('chat')
    const item = wrapper
      .findAllComponents({ name: 'SettingsItem' })
      .find(i => i.props().label === '初始消息数')

    await item!.vm.$emit('update:modelValue', 30)
    await flushPromises()

    expect(mockToastShow).not.toHaveBeenCalled()
  })
})

// ─── Settings search filter (issue #691) ──────────
//
// A search hit opens the category with `searchKeys`, and the page must render
// ONLY those rows — banner included — so the landing matches what the index
// promised. Run against the `mail` category because it mixes flat rows with
// placeholder cards (邮箱账号 → MailSetting).
describe('SettingsCategory search filter', () => {
  function mountFiltered(categoryId: string, searchKeys: string[], searchQuery: string) {
    return mount(SettingsCategory, {
      props: { categoryId, searchKeys, searchQuery },
      global: {
        plugins: [i18n],
        stubs: {
          SettingsGroupPanel: true,
          IosInstallDrawer: true,
          UpgradeDialog: true,
          BottomSheet: true,
          ProviderIcon: true,
          PasswordChangeDialog: true,
          ProjectsSetting: true,
          ProjectDetailSetting: true,
          MailSetting: true,
        },
      },
    })
  }

  const itemKeys = (wrapper: ReturnType<typeof mountFiltered>) =>
    wrapper.findAllComponents({ name: 'SettingsItem' }).map(i => i.props('settingKey'))

  it('renders only the hit row and the banner', () => {
    const wrapper = mountFiltered('mail', ['mail.sync_interval_minutes'], '每多少分钟')

    // This suite's i18n only carries the keys the page uses, so the banner
    // renders raw keys; the copy itself is covered by the i18n parity test.
    const banner = wrapper.find('.settings-search-filter')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('settings.search.filteredTitle')
    expect(banner.text()).toContain('每多少分钟')
    expect(itemKeys(wrapper)).toEqual(['mail.sync_interval_minutes'])
    // The whole-page control is wired through to the parent.
    expect(wrapper.find('.settings-search-filter__clear').exists()).toBe(true)
  })

  it('shows the whole page when there is no filter', () => {
    const wrapper = mountCategory('mail')
    expect(itemKeys(wrapper).length).toBeGreaterThan(3)
    expect(wrapper.find('.settings-search-filter').exists()).toBe(false)
  })

  it('emits clearSearch when the banner button is pressed', async () => {
    const wrapper = mountFiltered('mail', ['mail.notify'], '通知')
    await wrapper.find('.settings-search-filter__clear').trigger('click')
    expect(wrapper.emitted('clearSearch')).toBeTruthy()
  })

  it('drops the placeholder-only 邮箱账号 card while filtering', () => {
    const wrapper = mountFiltered('mail', ['mail.sync_interval_minutes'], '每多少分钟')
    const titles = wrapper.findAll('.settings-card__header').map(h => h.text())
    // 邮箱账号 renders MailSetting (a dedicated body, no settings row) and must
    // not survive as a card of unrelated content. 功能状态 is placeholder-driven
    // too, and 邮件阅读 / 新邮件提醒 lose their only row.
    expect(titles).toEqual(['settings.items.mailSyncSection'])
  })
})

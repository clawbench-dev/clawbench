import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { createI18n } from 'vue-i18n'
import ChatInputBar from '../ChatInputBar.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        actions: {
          session: 'Sessions',
          userMsgIndex: 'Index',
          deleteCurrentSession: 'Delete',
          noSessionToDelete: 'No session',
          autoSpeech: 'Auto speech',
          attachment: 'Attach',
        },
        create: { selectAgentOrLongPress: 'New' },
        input: {
          placeholder: 'Type a message...',
          clearInput: 'Clear',
          quickMenu: 'Quick',
          enqueue: 'Queue',
          send: 'Send',
          confirmStop: 'Confirm stop',
          stopGenerating: 'Stop',
        },
        attach: {
          dropToUpload: 'Drop to upload',
          openFile: 'Open',
          uploadFile: 'Upload',
          currentFile: 'Current file',
          currentDir: 'Current dir',
          recentReferences: 'Recent',
          uploading: 'Uploading...',
        },
        quickSend: {
          title: 'Quick send',
          edit: 'Edit',
        },
      },
      common: { copy: 'Copy', remove: 'Remove', cancel: 'Cancel' },
    },
  },
})

// Mock all composables
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useChatContext.ts', () => ({
  useChatContext: () => ({
    attachedFiles: [],
    addAttachedFile: vi.fn(),
    removeAttachedFile: vi.fn(),
    hasAttachedFile: () => false,
  }),
}))

vi.mock('@/composables/useChatStream.ts', () => ({
  useChatStream: () => ({
    loading: { value: false },
    cancelling: { value: false },
    stopPrimed: { value: false },
  }),
}))

vi.mock('@/composables/useQuoteQuestion.ts', () => ({
  useQuoteQuestion: () => ({
    quoteData: { value: null },
  }),
}))

vi.mock('@/composables/useFileUpload.ts', () => ({
  useFileUpload: () => ({
    pendingFiles: { value: [] },
    uploadingFiles: { value: [] },
    isDragOver: { value: false },
    onDragEnter: vi.fn(),
    onDragOver: vi.fn(),
    onDragLeave: vi.fn(),
    onDrop: vi.fn(),
    onFileSelect: vi.fn(),
    triggerUpload: vi.fn(),
    removePendingFile: vi.fn(),
  }),
}))

vi.mock('@/composables/useAutoSpeech.ts', () => ({
  useAutoSpeech: () => ({
    autoSpeechEnabled: { value: false },
  }),
}))

// Mock useQuickSend - must return items as a ref since component destructures it
const mockQuickSendItems = { value: [] }
vi.mock('@/composables/useQuickSend.ts', () => ({
  useQuickSend: () => ({
    items: mockQuickSendItems,
    loaded: { value: true },
    showEditDialog: { value: false },
    fetchItems: vi.fn(),
    addItem: vi.fn(),
    updateItem: vi.fn(),
    deleteItem: vi.fn(),
    reorderItems: vi.fn(),
  }),
}))

vi.mock('@/composables/useLocale.ts', () => ({
  gt: (key: string) => key,
}))

vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: { value: false },
  }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      currentFile: null,
      currentDir: '',
      chatUnreadCount: 0,
      chatRunning: false,
    },
  },
}))

vi.mock('@/utils/path.ts', () => ({
  baseName: (p: string) => p.split('/').pop() || '',
}))

vi.mock('@/utils/fileAttachmentUtils.ts', () => ({
  isImageFile: () => false,
  isUploadPath: () => false,
  normalizeFileEntry: (f: any) => f,
}))

vi.mock('@/utils/fileManager.ts', () => ({
  isThumbableExt: () => false,
}))

const stubs = {
  PopupMenu: { template: '<div><slot /></div>' },
  SessionSettingModal: true,
  List: true,
  Plus: true,
  Trash2: true,
  Volume2: true,
  MessagesSquare: true,
  Paperclip: true,
  XCircle: true,
  Send: true,
  Zap: true,
  Inbox: true,
  Square: true,
  Loader2: true,
  FileText: true,
  Folder: true,
  Upload: true,
  MessageSquare: true,
}

describe('ChatInputBar', () => {
  function mountBar(props = {}) {
    return mount(ChatInputBar, {
      props: {
        inputDisabled: false,
        currentSessionId: '',
        currentAgentId: '',
        attachedFiles: [],
        pendingFiles: [],
        ...props,
      },
      global: {
        plugins: [i18n],
        stubs,
      },
    })
  }

  it('renders the input wrapper', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.chat-input-wrapper').exists()).toBe(true)
  })

  it('renders the textarea', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.chat-textarea').exists()).toBe(true)
  })

  it('renders the attach button', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.chat-attach-btn').exists()).toBe(true)
  })

  it('renders the send button', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.chat-send-btn').exists()).toBe(true)
  })

  it('renders the top action bar', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.chat-top-actions').exists()).toBe(true)
  })

  it('renders the session action button', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.chat-action-btn').exists()).toBe(true)
  })
})

// Global application state (singleton reactive store)
import { reactive } from 'vue'
import { apiGet, apiPost } from '@/utils/api'
import { baseName, dirName } from '@/utils/path.ts'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'

interface DirEntry {
    name: string
    type: 'dir' | 'file'
    size?: number
    modTime?: string
}

interface CurrentFile {
    name: string
    path: string
    content?: string | null
    isImage?: boolean
    isPdf?: boolean
    isAudio?: boolean
    isVideo?: boolean
    isHtml?: boolean
    isBinary?: boolean
    tooLarge?: boolean
    size?: number
    error?: string
}

interface AppState {
    // Project
    projectRoot: string
    projectName: string
    rootPaths: string[]
    homeDir: string

    // Upload config
    uploadMaxSizeMB: number
    uploadMaxFiles: number

    // Chat UI config
    chatInitialMessages: number
    chatPageSize: number
    chatSessionPageSize: number
    chatCollapsedHeight: number
    sessionMaxCount: number
    sessionCount: number

    // Recent projects config
    recentProjectsMaxCount: number

    // Chat unread badge
    chatUnread: boolean

    // Chat running indicator (AI is generating)
    chatRunning: boolean

    // Task unread badge (unread task executions)
    taskUnread: boolean

    // Task running indicator (scheduled task is executing)
    taskRunning: boolean

    // Task just completed (brief flash for dock button animation)
    taskJustCompleted: boolean

    // Task list (kept in sync by global polling)
    tasks: any[]

    // File browser
    currentDir: string
    dirEntries: DirEntry[]
    dirLoading: boolean

    // Current file
    currentFile: CurrentFile | null

    // Theme
    theme: string

    // Git
    gitBranch: string
    gitHead: string
    gitDirty: boolean

}

const state = reactive<AppState>({
    // Project
    projectRoot: '',
    projectName: '',
    rootPaths: [],
    homeDir: '',

    // Upload config
    uploadMaxSizeMB: 100,
    uploadMaxFiles: 20,

    // Chat UI config
    chatInitialMessages: 20,
    chatPageSize: 20,
    chatSessionPageSize: 10,
    chatCollapsedHeight: 150,
    sessionMaxCount: 10,
    sessionCount: 0,
    recentProjectsMaxCount: 10,
    chatUnread: false,
    chatRunning: false,
    taskUnread: false,
    taskRunning: false,
    taskJustCompleted: false,
    tasks: [],

    // File browser
    currentDir: '',
    dirEntries: [],
    dirLoading: false,

    // Current file
    currentFile: null,

    // Theme
    theme: 'light',

    // Git
    gitBranch: '',
    gitHead: '',
    gitDirty: false,

})

// =============================================
// Project
// =============================================

async function loadProject(): Promise<void> {
    try {
        try {
            const wd = await apiGet<{ roots: string[]; uploadMaxSizeMB: number; uploadMaxFiles: number; chatInitialMessages?: number; chatPageSize?: number; chatSessionPageSize?: number; chatCollapsedHeight?: number; sessionMaxCount?: number; recentProjectsMaxCount?: number }>('/api/roots')
            state.rootPaths = wd.roots || []
            if (wd.uploadMaxSizeMB > 0) state.uploadMaxSizeMB = wd.uploadMaxSizeMB
            if (wd.uploadMaxFiles > 0) state.uploadMaxFiles = wd.uploadMaxFiles
            if (wd.chatInitialMessages > 0) state.chatInitialMessages = wd.chatInitialMessages
            if (wd.chatPageSize > 0) state.chatPageSize = wd.chatPageSize
            if (wd.chatSessionPageSize > 0) state.chatSessionPageSize = wd.chatSessionPageSize
            if (wd.chatCollapsedHeight > 0) state.chatCollapsedHeight = wd.chatCollapsedHeight
            if (wd.sessionMaxCount > 0) state.sessionMaxCount = wd.sessionMaxCount
            if (wd.recentProjectsMaxCount > 0) state.recentProjectsMaxCount = wd.recentProjectsMaxCount
        } catch (error) {
            console.error('[loadProject] roots failed:', error)
        }
        const data = await apiGet<{ path: string; homeDir?: string }>('/api/project')
        if (!data.path) return
        state.projectRoot = data.path
        state.projectName = baseName(data.path)
        state.homeDir = data.homeDir || ''
        localStorage.setItem('currentProjectPath', data.path)
        // Add to recent projects
        apiPost('/api/recent-projects', { path: data.path }).catch(() => {})
    } catch (error) {
        console.error('[loadProject] failed:', error)
    }
}

async function setProject(path: string): Promise<string> {
    const data = await apiPost<{ ok: string; path: string }>('/api/project', { path })
    resetProjectState()
    return data.path || path
}

function resetProjectState(): void {
    // Project
    state.projectRoot = ''
    state.projectName = ''
    state.rootPaths = []
    state.homeDir = ''
    // File browser
    state.currentDir = ''
    state.dirEntries = []
    state.dirLoading = false
    state.currentFile = null
    // Git
    state.gitBranch = ''
    state.gitHead = ''
    state.gitDirty = false
    // Chat/task badges
    state.chatUnread = false
    state.chatRunning = false
    state.taskUnread = false
    state.taskRunning = false
    state.taskJustCompleted = false
    state.tasks = []
    // Config defaults
    state.uploadMaxSizeMB = 100
    state.uploadMaxFiles = 20
    state.chatInitialMessages = 20
    state.chatPageSize = 20
    state.chatSessionPageSize = 10
    state.chatCollapsedHeight = 150
    state.sessionMaxCount = 10
    state.sessionCount = 0
    state.recentProjectsMaxCount = 10
}

// =============================================
// Git
// =============================================

async function loadGitBranch(): Promise<{ isGit: boolean; branch: string; head: string; dirty: boolean }> {
    try {
        const data = await apiGet<{ isGit: boolean; branch: string; head: string; dirty: boolean }>('/api/git/branch')
        state.gitBranch = data.branch || ''
        state.gitHead = data.head || ''
        state.gitDirty = !!data.dirty
        return data
    } catch (_) {
        state.gitBranch = ''
        state.gitHead = ''
        state.gitDirty = false
        return { isGit: false, branch: '', head: '', dirty: false }
    }
}

// =============================================
// File browser
// =============================================

let loadFilesSeq = 0 // monotonic counter to suppress stale concurrent loads

async function loadFiles(dir = ''): Promise<void> {
    const seq = ++loadFilesSeq // this call supersedes any earlier in-flight call
    const prevDir = state.currentDir
    const prevEntries = state.dirEntries.slice()
    state.dirLoading = true
    try {
        const url = dir ? `/api/dir?path=${encodeURIComponent(dir)}` : '/api/dir?path='
        const data = await apiGet<{ items: DirEntry[] }>(url)
        // A newer loadFiles call started while we were awaiting — discard our result
        if (seq !== loadFilesSeq) return
        state.currentDir = dir
        state.dirEntries = data.items || []
    } catch (err) {
        // A newer loadFiles call started — don't corrupt its state
        if (seq !== loadFilesSeq) return
        // Roll back to previous state on failure
        state.currentDir = prevDir
        state.dirEntries = prevEntries
        useToast().show(gt('file.toast.dirLoadFailed'), { type: 'error', icon: '⚠️' })
    } finally {
        // Only clear loading if we are still the latest call
        if (seq === loadFilesSeq) {
            state.dirLoading = false
        }
    }
}

async function selectFile(path: string, isImageFile = false, isAudioFile = false, addToHistory = true, forceText = false): Promise<void> {
    const key = 'clawbenchLastFile_' + state.projectRoot
    if (key !== 'clawbenchLastFile_') localStorage.setItem(key, path)

    // Detect media files by extension (avoids dynamic import)
    const imageExts = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.bmp', '.ico', '.tiff', '.tif', '.avif']
    const audioExts = ['.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.wma', '.opus']
    const videoExts = ['.mp4', '.mkv', '.avi', '.mov', '.webm', '.flv', '.wmv', '.m4v', '.3gp', '.m3u8']
    // Only fetch content for known text file extensions; everything else is binary.
    // This must match the backend model.IsTextFile() list.
    const textExts = [
        '.md', '.markdown',
        '.json', '.jsonc', '.json5',
        '.yaml', '.yml',
        '.toml',
        '.xml', '.plist',
        '.ini', '.properties', '.conf', '.cfg',
        '.go', '.mod', '.sum',
        '.py', '.pyi',
        '.rs',
        '.js', '.mjs', '.cjs',
        '.ts', '.tsx', '.mts', '.cts',
        '.java',
        '.cs',
        '.rb',
        '.php',
        '.swift',
        '.kt', '.kts',
        '.scala',
        '.c', '.h', '.cpp', '.hpp', '.cc', '.cxx',
        '.lua',
        '.r', '.R',
        '.pl', '.pm',
        '.sh', '.bash', '.zsh', '.fish', '.ksh', '.ash',
        '.ps1', '.psm1',
        '.sql',
        '.graphql', '.gql',
        '.html', '.htm', '.xhtml',
        '.css', '.scss', '.sass', '.less', '.styl',
        '.vue', '.svelte',
        '.dockerfile', '.dockerignore',
        '.makefile', '.mak',
        '.nginx',
        '.gitignore', '.gitattributes', '.gitconfig',
        '.editorconfig',
        '.env', '.env.example', '.env.local',
        '.ignore',
        '.txt', '.text',
        '.log',
        '.diff', '.patch',
        '.csv', '.tsv',
        '.tex',
        '.pem', '.crt', '.key', '.pub',
        '.regex', '.regexp',
    ]
    const lower = path.toLowerCase()
    const isPdf = lower.endsWith('.pdf')
    const isImage = isImageFile || imageExts.some(ext => lower.endsWith(ext))
    const isAudio = isAudioFile || audioExts.some(ext => lower.endsWith(ext))
    const isVideo = videoExts.some(ext => lower.endsWith(ext))
    const isText = textExts.some(ext => lower.endsWith(ext))
    if (isPdf) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isPdf: true }
        return
    }
    if (isImage) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isImage: true }
        return
    }
    if (isAudio) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isAudio: true }
        return
    }
    if (isVideo) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isVideo: true }
        return
    }
    if (!isText && !forceText) {
        // Unknown extension → treat as binary, don't even call the API
        const fileName = baseName(path)
        const sizeInfo = state.dirEntries.find(e => e.name === fileName)
        state.currentFile = { name: fileName, path, content: null, isBinary: true, size: sizeInfo?.size }
        return
    }

    try {
        // Strip leading slash to prevent double-slash URLs (/api/file//path)
        // which Go's ServeMux decodes from %2F, causing InvalidFilePath errors.
        const cleanPath = path.replace(/^\/+/, '')
        const url = forceText && !isText
            ? `/api/file/${encodeURIComponent(cleanPath)}?forceText=1`
            : `/api/file/${encodeURIComponent(cleanPath)}`
        const resp = await fetch(url)
        if (!resp.ok) {
            const err = await resp.json() as { error?: string, msgKey?: string }
            if (err.msgKey === 'FileTooLarge') {
                const fileName = baseName(path)
                const sizeInfo = state.dirEntries.find(e => e.name === fileName)
                state.currentFile = { name: fileName, path, content: null, tooLarge: true, size: sizeInfo?.size }
                return
            }
            throw new Error(err.error || 'Failed')
        }
        const data = await resp.json() as CurrentFile
        // When forceText=true, backend omits isBinary:false (Go zero value).
        // Must explicitly clear it so the binary fallback view disappears.
        if (forceText) {
            data.isBinary = false
            data.tooLarge = false
        }
        // Detect HTML files for preview mode
        const htmlExts = ['.html', '.htm', '.xhtml']
        if (htmlExts.some(ext => lower.endsWith(ext))) {
            data.isHtml = true
        }
        // Backend may also mark as binary if the file somehow passes frontend check
        // When refreshing the same file (auto-refresh from file watcher),
        // update content in-place to avoid a full object replacement which
        // causes visual flash (v-html teardown/rebuild in MarkdownPreview).
        if (state.currentFile?.path === path && !addToHistory) {
            Object.assign(state.currentFile, data)
        } else {
            state.currentFile = data
        }
    } catch (err) {
        state.currentFile = { path, name: baseName(path), error: (err as Error).message }
    }
}

async function deleteFile(filePath: string): Promise<void> {
    if (!await useDialog().confirm(gt('file.header.confirmDelete', { name: baseName(filePath) }), { dangerous: true })) return
    await apiPost('/api/file/delete', { path: filePath })
    if (state.currentFile?.path === filePath) {
        state.currentFile = null
    }
    await loadFiles(state.currentDir)
}

async function deleteFiles(paths: string[]): Promise<void> {
    if (!paths.length) return
    await Promise.all(paths.map(p => apiPost('/api/file/delete', { path: p })))
    if (state.currentFile && paths.includes(state.currentFile.path)) {
        state.currentFile = null
    }
    await loadFiles(state.currentDir)
}

async function renameFile(path: string, newName: string): Promise<void> {
    await apiPost('/api/file/rename', { path, name: newName })
    await loadFiles(state.currentDir)
}

// =============================================
// Navigation
// =============================================

async function navigateToDir(dirPath: string): Promise<void> {
    await loadFiles(dirPath)
}

export const store = {
    state,
    loadProject,
    setProject,
    resetProjectState,
    loadGitBranch,
    loadFiles,
    selectFile,
    deleteFile,
    deleteFiles,
    renameFile,
    navigateToDir,
}

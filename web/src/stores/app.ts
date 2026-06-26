// Global application state (singleton reactive store)
import { reactive } from 'vue'
import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { baseName, dirName } from '@/utils/path.ts'
import { gt } from '@/composables/useLocale'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'

const TAG = 'Store'

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
    truncated?: boolean
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
    sessionMaxCount: number
    sessionCount: number

    // Recent projects config
    recentProjectsMaxCount: number

    // Chat unread count (for dock badge — number of unread sessions)
    chatUnreadCount: number

    // Chat running indicator (AI is generating)
    chatRunning: boolean

    // Task unread count (for dock badge)
    taskUnreadCount: number

    // Task running indicator (scheduled task is executing)
    taskRunning: boolean

    // Task just completed (brief flash for dock button animation)
    taskJustCompleted: boolean

    // Terminal session count (for dock badge)
    terminalSessionCount: number

    // Active port forward count (for dock badge)
    portForwardActiveCount: number

    // Task list (kept in sync by global polling)
    tasks: any[]

    // File browser
    currentDir: string
    dirEntries: DirEntry[]
    dirLoading: boolean
    fileLoading: boolean

    // Current file
    currentFile: CurrentFile | null

    // Theme
    theme: string

    // Git
    gitBranch: string
    gitHead: string
    gitDirty: boolean
    gitWorkingTreeChangeCount: number

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
    sessionMaxCount: 10,
    sessionCount: 0,
    recentProjectsMaxCount: 10,
    chatUnreadCount: 0,
    chatRunning: false,
    taskUnreadCount: 0,
    taskRunning: false,
    taskJustCompleted: false,
    terminalSessionCount: 0,
    portForwardActiveCount: 0,
    tasks: [],

    // File browser
    currentDir: '',
    dirEntries: [],
    dirLoading: false,
    fileLoading: false,

    // Current file
    currentFile: null,

    // Theme
    theme: 'light',

    // Git
    gitBranch: '',
    gitHead: '',
    gitDirty: false,
    gitWorkingTreeChangeCount: 0,

})

// =============================================
// Project
// =============================================

async function loadProject(): Promise<void> {
    try {
        try {
            const wd = await apiGet<{ roots: string[]; uploadMaxSizeMB: number; uploadMaxFiles: number; chatInitialMessages?: number; chatPageSize?: number; chatSessionPageSize?: number; sessionMaxCount?: number; recentProjectsMaxCount?: number }>('/api/roots')
            state.rootPaths = wd.roots || []
            if (wd.uploadMaxSizeMB > 0) state.uploadMaxSizeMB = wd.uploadMaxSizeMB
            if (wd.uploadMaxFiles > 0) state.uploadMaxFiles = wd.uploadMaxFiles
            if ((wd.chatInitialMessages ?? 0) > 0) state.chatInitialMessages = wd.chatInitialMessages!
            if ((wd.chatPageSize ?? 0) > 0) state.chatPageSize = wd.chatPageSize!
            if ((wd.chatSessionPageSize ?? 0) > 0) state.chatSessionPageSize = wd.chatSessionPageSize!
            if ((wd.sessionMaxCount ?? 0) > 0) state.sessionMaxCount = wd.sessionMaxCount!
            if ((wd.recentProjectsMaxCount ?? 0) > 0) state.recentProjectsMaxCount = wd.recentProjectsMaxCount!
        } catch (error) {
            appLog.e(TAG, '[loadProject] roots failed:', error)
        }
        const data = await apiGet<{ path: string; homeDir?: string }>('/api/project')
        if (!data.path) return
        state.projectRoot = data.path
        state.projectName = baseName(data.path)
        state.homeDir = data.homeDir || ''
        localStorage.setItem('currentProjectPath', data.path)
    } catch (error) {
        appLog.e(TAG, '[loadProject] failed:', error)
    }
}

async function setProject(path: string): Promise<string> {
    const data = await apiPost<{
        ok: string; path: string; homeDir?: string
        roots?: string[]; uploadMaxSizeMB?: number; uploadMaxFiles?: number
        chatInitialMessages?: number; chatPageSize?: number; chatSessionPageSize?: number
        sessionMaxCount?: number; recentProjectsMaxCount?: number
    }>('/api/project', { path })
    resetProjectState()
    // Apply expanded response from POST — eliminates follow-up GET /api/roots + GET /api/project
    if (data.path) {
        state.projectRoot = data.path
        state.projectName = baseName(data.path)
        localStorage.setItem('currentProjectPath', data.path)
    }
    if (data.homeDir) state.homeDir = data.homeDir
    if (data.roots?.length) state.rootPaths = data.roots
    if ((data as any).uploadMaxSizeMB > 0) state.uploadMaxSizeMB = (data as any).uploadMaxSizeMB
    if ((data as any).uploadMaxFiles > 0) state.uploadMaxFiles = (data as any).uploadMaxFiles
    if ((data as any).chatInitialMessages > 0) state.chatInitialMessages = (data as any).chatInitialMessages
    if ((data as any).chatPageSize > 0) state.chatPageSize = (data as any).chatPageSize
    if ((data as any).chatSessionPageSize > 0) state.chatSessionPageSize = (data as any).chatSessionPageSize
    if ((data as any).sessionMaxCount > 0) state.sessionMaxCount = (data as any).sessionMaxCount
    if ((data as any).recentProjectsMaxCount > 0) state.recentProjectsMaxCount = (data as any).recentProjectsMaxCount
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
    state.fileLoading = false
    state.currentFile = null
    // Git
    state.gitBranch = ''
    state.gitHead = ''
    state.gitDirty = false
    state.gitWorkingTreeChangeCount = 0
    // Chat/task badges
    state.chatUnreadCount = 0
    state.chatRunning = false
    state.taskUnreadCount = 0
    state.taskRunning = false
    state.taskJustCompleted = false
    state.terminalSessionCount = 0
    state.portForwardActiveCount = 0
    state.tasks = []
    // Config defaults
    state.uploadMaxSizeMB = 100
    state.uploadMaxFiles = 20
    state.chatInitialMessages = 20
    state.chatPageSize = 20
    state.chatSessionPageSize = 10
    state.sessionMaxCount = 10
    state.sessionCount = 0
    state.recentProjectsMaxCount = 10
}

// =============================================
// Git
// =============================================

async function loadGitBranch(): Promise<{ isGit: boolean; branch: string; head: string; dirty: boolean; changeCount: number }> {
    try {
        const data = await apiGet<{ isGit: boolean; branch: string; head: string; dirty: boolean; changeCount: number }>('/api/git/branch')
        state.gitBranch = data.branch || ''
        state.gitHead = data.head || ''
        state.gitDirty = !!data.dirty
        state.gitWorkingTreeChangeCount = data.changeCount || 0
        return data
    } catch (_) {
        state.gitBranch = ''
        state.gitHead = ''
        state.gitDirty = false
        state.gitWorkingTreeChangeCount = 0
        return { isGit: false, branch: '', head: '', dirty: false, changeCount: 0 }
    }
}

// =============================================
// File browser
// =============================================

let loadFilesSeq = 0 // monotonic counter to suppress stale concurrent loads
let selectFileSeq = 0 // monotonic counter to suppress stale concurrent file loads

async function loadFiles(dir = ''): Promise<void> {
    const seq = ++loadFilesSeq // this call supersedes any earlier in-flight call
    // Defensive: strip leading slashes so currentDir is always a project-relative path.
    // The Go backend treats paths starting with "/" as absolute filesystem paths,
    // which causes 500 errors when they're not under configured root paths.
    dir = dir.replace(/^\/+/, '')
    const prevDir = state.currentDir
    const prevEntries = state.dirEntries.slice()
    state.dirLoading = true
    try {
        const url = dir ? `/api/dir?path=${encodeURIComponent(dir)}` : '/api/dir?path='
        const data = await apiGet<{ items: DirEntry[] }>(url)
        // A newer loadFiles call started while we were awaiting — discard our result
        if (seq !== loadFilesSeq) {
            appLog.d(TAG, `[loadFiles] seq=${seq} discarded (current=${loadFilesSeq})`)
            return
        }
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

async function selectFile(path: string, isImageFile = false, isAudioFile = false, addToHistory = true, forceText = false): Promise<boolean> {
    const seq = ++selectFileSeq // this call supersedes any earlier in-flight call

    // Detect media files by extension (avoids dynamic import)
    const imageExts = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.bmp', '.ico', '.tiff', '.tif', '.avif']
    const audioExts = ['.mp3', '.wav', '.ogg', '.m4a', '.aac', '.flac', '.wma', '.opus']
    const videoExts = ['.mp4', '.mkv', '.avi', '.mov', '.webm', '.flv', '.wmv', '.m4v', '.3gp', '.m3u8']
    const lower = path.toLowerCase()
    const isPdf = lower.endsWith('.pdf')
    const isImage = isImageFile || imageExts.some(ext => lower.endsWith(ext))
    const isAudio = isAudioFile || audioExts.some(ext => lower.endsWith(ext))
    const isVideo = videoExts.some(ext => lower.endsWith(ext))
    if (isPdf) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isPdf: true }
        return true
    }
    if (isImage) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isImage: true }
        return true
    }
    if (isAudio) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isAudio: true }
        return true
    }
    if (isVideo) {
        const fileName = baseName(path)
        state.currentFile = { name: fileName, path, content: null, isVideo: true }
        return true
    }

    try {
        // Absolute paths (project-external) use query parameter to avoid URL path
        // encoding issues: encodeURIComponent("/path") produces %2Fpath which
        // Go's ServeMux decodes back to /, making it look like a relative path.
        // Project-internal relative paths continue to use URL path encoding.
        state.fileLoading = true
        const isAbsPath = path.startsWith('/')
        let url: string
        if (isAbsPath) {
            url = forceText
                ? `/api/file?path=${encodeURIComponent(path)}&forceText=1`
                : `/api/file?path=${encodeURIComponent(path)}`
        } else {
            // Strip leading slash to prevent double-slash URLs (/api/file//path)
            // which Go's ServeMux decodes from %2F, causing InvalidFilePath errors.
            const cleanPath = path.replace(/^\/+/, '')
            url = forceText
                ? `/api/file/${encodeURIComponent(cleanPath)}?forceText=1`
                : `/api/file/${encodeURIComponent(cleanPath)}`
        }
        const resp = await fetch(url)
        if (!resp.ok) {
            const err = await resp.json() as { error?: string, msgKey?: string }
            if (err.msgKey === 'FileTooLarge') {
                const fileName = baseName(path)
                const sizeInfo = state.dirEntries.find(e => e.name === fileName)
                state.currentFile = { name: fileName, path, content: null, tooLarge: true, size: sizeInfo?.size }
                return true
            }
            throw new Error(err.error || 'Failed')
        }
        const data = await resp.json() as CurrentFile
        // When forceText=true, clear isBinary/tooLarge so binary fallback disappears
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
        return true
    } catch (err) {
        // Don't replace currentFile — keep the previously opened file visible.
        useToast().show((err as Error).message, { type: 'error', icon: '⚠️' })
        return false
    } finally {
        if (seq === selectFileSeq) {
            state.fileLoading = false
        }
    }
}

async function deleteFile(filePath: string): Promise<void> {
    appLog.d(TAG, '[deleteFile] start:', filePath)
    const confirmed = await useDialog().confirm(gt('file.header.confirmDelete', { name: baseName(filePath) }), { dangerous: true })
    appLog.d(TAG, '[deleteFile] dialog result:', confirmed)
    if (!confirmed) {
        appLog.d(TAG, '[deleteFile] user cancelled')
        return
    }
    try {
        await apiPost('/api/file/delete', { path: filePath })
        appLog.d(TAG, '[deleteFile] API success')
    } catch (err) {
        // File not found = already deleted (e.g. concurrent delete), treat as success
        const msgKey = (err as Error & { msgKey?: string })?.msgKey
        if (msgKey !== 'FileNotFoundShort') {
            appLog.e(TAG, '[deleteFile] API error:', err)
            useToast().show(gt('file.toast.deleteFailed'), { type: 'error', icon: '⚠️' })
        } else {
            appLog.d(TAG, '[deleteFile] file already gone (404), treating as success')
        }
    }
    if (state.currentFile?.path === filePath) {
        state.currentFile = null
    }
    appLog.d(TAG, '[deleteFile] refreshing, currentDir:', state.currentDir, 'loadFilesSeq:', loadFilesSeq)
    await Promise.all([loadFiles(state.currentDir), loadGitBranch()])
    appLog.d(TAG, '[deleteFile] done, dirEntries count:', state.dirEntries.length)
}

async function deleteFiles(paths: string[]): Promise<void> {
    if (!paths.length) return
    appLog.d(TAG, '[deleteFiles] start:', paths.length, 'files')
    const results = await Promise.allSettled(paths.map(p => apiPost('/api/file/delete', { path: p })))
    const realFailures = results.filter(r => {
        if (r.status !== 'rejected') return false
        const msgKey = ((r as PromiseRejectedResult).reason as Error & { msgKey?: string })?.msgKey
        return msgKey !== 'FileNotFoundShort' // already deleted = not a real failure
    })
    if (realFailures.length) {
        appLog.e(TAG, '[deleteFiles] some deletes failed:', realFailures.map(r => (r as PromiseRejectedResult).reason))
        useToast().show(gt('file.toast.deleteFailed'), { type: 'error', icon: '⚠️' })
    }
    if (state.currentFile && paths.includes(state.currentFile.path)) {
        state.currentFile = null
    }
    await Promise.all([loadFiles(state.currentDir), loadGitBranch()])
    appLog.d(TAG, '[deleteFiles] done, dirEntries count:', state.dirEntries.length)
}

async function renameFile(path: string, newName: string): Promise<void> {
    try {
        await apiPost('/api/file/rename', { path, name: newName })
    } catch (err) {
        const msgKey = (err as Error & { msgKey?: string })?.msgKey
        if (msgKey === 'FileNotFoundShort') {
            // File already gone — treat as success
        } else {
            const toast = useToast()
            toast.show(gt('file.toast.renameFailed') || 'Rename failed', { icon: '❌', type: 'error', duration: 2000 })
            throw err
        }
    }
    // If the renamed file is currently being viewed, re-select it at the new path
    if (state.currentFile?.path === path) {
        const dir = dirName(path)
        const newPath = dir ? `${dir}/${newName}` : newName
        await selectFile(newPath)
    }
    await loadFiles(state.currentDir)
}

// =============================================
// Directory navigation
// =============================================

async function navigateToDir(path: string): Promise<void> {
    if (state.dirLoading) return
    await loadFiles(path)
}

async function navigateToParentDir(): Promise<void> {
    if (state.dirLoading) return
    if (state.currentDir === '') return // already at project root, nothing to go back to
    const parent = dirName(state.currentDir)
    await loadFiles(parent)
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
    navigateToParentDir,
}

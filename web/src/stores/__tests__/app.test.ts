import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock apiGet and apiPost
const mockApiPost = vi.fn()
const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
    apiGet: (...args: any[]) => mockApiGet(...args),
    apiPost: (...args: any[]) => mockApiPost(...args),
}))

// Mock path utils
vi.mock('@/utils/path.ts', () => ({
    baseName: (p: string) => p.split('/').pop() || '',
    dirName: (p: string) => { const parts = p.split('/'); parts.pop(); return parts.join('/') },
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key,
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: mockToastShow }),
}))

// Mock useDialog
const mockDialogConfirm = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/useDialog', () => ({
    useDialog: () => ({ confirm: mockDialogConfirm }),
}))

import { store } from '@/stores/app'

describe('store', () => {
    beforeEach(() => {
        vi.clearAllMocks()
        // Reset state to defaults before each test
        store.resetProjectState()
    })

    // ── resetProjectState ──

    describe('resetProjectState', () => {
        it('clears project fields', () => {
            store.state.projectRoot = '/some/project'
            store.state.projectName = 'project'
            store.state.rootPaths = ['/']

            store.resetProjectState()

            expect(store.state.projectRoot).toBe('')
            expect(store.state.projectName).toBe('')
            expect(store.state.rootPaths).toEqual([])
        })

        it('clears file browser state', () => {
            store.state.currentDir = '/some/dir'
            store.state.dirEntries = [{ name: 'file.ts', type: 'file' }] as any
            store.state.dirLoading = true
            store.state.fileLoading = true
            store.state.currentFile = { name: 'file.ts', path: '/file.ts' } as any

            store.resetProjectState()

            expect(store.state.currentDir).toBe('')
            expect(store.state.dirEntries).toEqual([])
            expect(store.state.dirLoading).toBe(false)
            expect(store.state.fileLoading).toBe(false)
            expect(store.state.currentFile).toBeNull()
        })

        it('clears git state', () => {
            store.state.gitBranch = 'main'
            store.state.gitHead = 'abc123'
            store.state.gitDirty = true

            store.resetProjectState()

            expect(store.state.gitBranch).toBe('')
            expect(store.state.gitHead).toBe('')
            expect(store.state.gitDirty).toBe(false)
        })

        it('clears chat/task badges', () => {
            store.state.chatUnreadCount = 3
            store.state.chatRunning = true
            store.state.taskUnreadCount = 5
            store.state.taskRunning = true
            store.state.taskJustCompleted = true
            store.state.tasks = [{ id: 'task-1' }]

            store.resetProjectState()

            expect(store.state.chatUnreadCount).toBe(0)
            expect(store.state.chatRunning).toBe(false)
            expect(store.state.taskUnreadCount).toBe(0)
            expect(store.state.taskRunning).toBe(false)
            expect(store.state.taskJustCompleted).toBe(false)
            expect(store.state.tasks).toEqual([])
        })

        it('resets config defaults', () => {
            store.state.uploadMaxSizeMB = 999
            store.state.uploadMaxFiles = 99
            store.state.chatInitialMessages = 999
            store.state.chatPageSize = 999
            store.state.chatSessionPageSize = 999
            store.state.sessionMaxCount = 999
            store.state.recentProjectsMaxCount = 999

            store.resetProjectState()

            expect(store.state.uploadMaxSizeMB).toBe(100)
            expect(store.state.uploadMaxFiles).toBe(20)
            expect(store.state.chatInitialMessages).toBe(20)
            expect(store.state.chatPageSize).toBe(20)
            expect(store.state.chatSessionPageSize).toBe(10)
            expect(store.state.sessionMaxCount).toBe(10)
            expect(store.state.recentProjectsMaxCount).toBe(10)
        })
    })

    // ── loadProject ──

    describe('loadProject', () => {
        it('reads recentProjectsMaxCount from roots API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') {
                    return { roots: ['/'], recentProjectsMaxCount: 5 }
                }
                if (url === '/api/project') {
                    return { path: '/home/user/project' }
                }
                return {}
            })
            mockApiPost.mockResolvedValue({})

            await store.loadProject()

            expect(store.state.recentProjectsMaxCount).toBe(5)
        })

        it('does not update recentProjectsMaxCount when API returns 0 or undefined', async () => {
            store.state.recentProjectsMaxCount = 10

            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') {
                    return { roots: ['/'], recentProjectsMaxCount: 0 }
                }
                if (url === '/api/project') {
                    return { path: '/home/user/project' }
                }
                return {}
            })
            mockApiPost.mockResolvedValue({})

            await store.loadProject()

            // 0 is not > 0, so it stays at the default set by resetProjectState
            expect(store.state.recentProjectsMaxCount).toBe(10)
        })

        it('sets projectRoot and projectName from /api/project', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'] }
                if (url === '/api/project') return { path: '/home/user/myproject' }
                return {}
            })
            mockApiPost.mockResolvedValue({})

            await store.loadProject()

            expect(store.state.projectRoot).toBe('/home/user/myproject')
            expect(store.state.projectName).toBe('myproject')
        })

        it('saves project path to localStorage', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'] }
                if (url === '/api/project') return { path: '/home/user/myproject' }
                return {}
            })
            mockApiPost.mockResolvedValue({})

            await store.loadProject()

            expect(localStorage.getItem('currentProjectPath')).toBe('/home/user/myproject')
        })

        it('does not set projectRoot when /api/project returns empty path', async () => {
            store.state.projectRoot = '/previous'

            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'] }
                if (url === '/api/project') return { path: '' }
                return {}
            })

            await store.loadProject()

            expect(store.state.projectRoot).toBe('/previous')
        })

        it('tolerates /api/roots failure and still loads project', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') throw new Error('network error')
                if (url === '/api/project') return { path: '/home/user/myproject' }
                return {}
            })
            mockApiPost.mockResolvedValue({})

            await store.loadProject()

            expect(store.state.projectRoot).toBe('/home/user/myproject')
            expect(store.state.projectName).toBe('myproject')
        })

        it('tolerates /api/project failure without throwing', async () => {
            store.state.projectRoot = '/previous'

            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'] }
                if (url === '/api/project') throw new Error('network error')
                return {}
            })

            // Should not throw — the error is caught internally
            await store.loadProject()

            // projectRoot stays as-is since /api/project failed
            expect(store.state.projectRoot).toBe('/previous')
        })

        it('tolerates both /api/roots and /api/project failing', async () => {
            store.state.projectRoot = '/previous'

            mockApiGet.mockImplementation(() => { throw new Error('network error') })

            // Should not throw — both errors are caught internally
            await store.loadProject()

            expect(store.state.projectRoot).toBe('/previous')
        })
    })

    // ── selectFile ──

    describe('selectFile', () => {
        it('strips leading slash from path to avoid double-slash URL', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: '/test.ts', content: 'hello' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('/test.ts')

            // Absolute paths use query parameter style to avoid encoding issues
            expect(mockFetch).toHaveBeenCalledWith('/api/file?path=%2Ftest.ts')
            vi.unstubAllGlobals()
        })

        it('strips multiple leading slashes from path', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: '///test.ts', content: 'hello' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('///test.ts')

            // Absolute paths use query parameter style
            expect(mockFetch).toHaveBeenCalledWith('/api/file?path=%2F%2F%2Ftest.ts')
            vi.unstubAllGlobals()
        })

        it('returns true for PDF files', async () => {
            const result = await store.selectFile('/doc.pdf')
            expect(result).toBe(true)
            expect(store.state.currentFile?.isPdf).toBe(true)
        })

        it('returns true for image files', async () => {
            const result = await store.selectFile('/photo.jpg')
            expect(result).toBe(true)
            expect(store.state.currentFile?.isImage).toBe(true)
        })

        it('returns true for audio files', async () => {
            const result = await store.selectFile('/song.mp3')
            expect(result).toBe(true)
            expect(store.state.currentFile?.isAudio).toBe(true)
        })

        it('returns true for video files', async () => {
            const result = await store.selectFile('/clip.mp4')
            expect(result).toBe(true)
            expect(store.state.currentFile?.isVideo).toBe(true)
        })

        it('returns isBinary when backend detects binary content', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'archive.zip', path: '/archive.zip', content: '', isBinary: true, size: 1024 }),
            })
            vi.stubGlobal('fetch', mockFetch)

            const result = await store.selectFile('/archive.zip')
            expect(result).toBe(true)
            expect(store.state.currentFile?.isBinary).toBe(true)
            expect(store.state.currentFile?.content).toBe('')
            vi.unstubAllGlobals()
        })

        it('uses forceText=1 to override binary detection', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'archive.zip', path: '/archive.zip', content: 'PK...', truncated: true }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('/archive.zip', false, false, true, true)

            expect(mockFetch).toHaveBeenCalledWith('/api/file?path=%2Farchive.zip&forceText=1')
            expect(store.state.currentFile?.isBinary).toBe(false)
            expect(store.state.currentFile?.content).toBe('PK...')
            expect(store.state.currentFile?.truncated).toBe(true)
            vi.unstubAllGlobals()
        })

        it('returns true for too-large files', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                json: () => Promise.resolve({ msgKey: 'FileTooLarge' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            const result = await store.selectFile('/huge.ts')
            expect(result).toBe(true)
            expect(store.state.currentFile?.tooLarge).toBe(true)

            vi.unstubAllGlobals()
        })

        it('returns false when API fetch fails', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                json: () => Promise.resolve({ error: 'not found' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            const result = await store.selectFile('/missing.ts')
            expect(result).toBe(false)

            vi.unstubAllGlobals()
        })

        it('sets fileLoading to true while loading a text file, then false', async () => {
            let resolveFetch: (v: any) => void
            const fetchPromise = new Promise(r => { resolveFetch = r })
            const mockFetch = vi.fn().mockReturnValue(fetchPromise)
            vi.stubGlobal('fetch', mockFetch)

            const selectPromise = store.selectFile('/test.ts')

            // While fetch is in flight, fileLoading should be true
            expect(store.state.fileLoading).toBe(true)

            // Resolve the fetch
            resolveFetch!({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: '/test.ts', content: 'hello' }),
            })

            await selectPromise

            // After fetch completes, fileLoading should be false
            expect(store.state.fileLoading).toBe(false)

            vi.unstubAllGlobals()
        })

        it('resets fileLoading to false when selectFile fails', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: false,
                json: () => Promise.resolve({ error: 'not found' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('/missing.ts')

            expect(store.state.fileLoading).toBe(false)

            vi.unstubAllGlobals()
        })

        it('does not set fileLoading for media files (instant)', async () => {
            await store.selectFile('/photo.jpg')

            // Media files don't enter the try block, so fileLoading stays false
            expect(store.state.fileLoading).toBe(false)
        })
    })

    // ── setProject ──

    describe('setProject', () => {
        it('calls API and resets project state', async () => {
            // Set some state that should be cleared
            store.state.projectRoot = '/old/project'
            store.state.gitBranch = 'old-branch'
            store.state.chatRunning = true

            mockApiPost.mockResolvedValue({ ok: 'ok', path: '/new/project' })

            const result = await store.setProject('/new/project')

            expect(mockApiPost).toHaveBeenCalledWith('/api/project', { path: '/new/project' })
            // After setProject, resetProjectState clears then applies new project data
            expect(store.state.projectRoot).toBe('/new/project')
            expect(store.state.gitBranch).toBe('')
            expect(store.state.chatRunning).toBe(false)
            // Returns the path from API response
            expect(result).toBe('/new/project')
        })

        it('returns the input path when API does not return a path', async () => {
            mockApiPost.mockResolvedValue({ ok: 'ok' })

            const result = await store.setProject('/my/project')

            expect(result).toBe('/my/project')
        })
    })

    // ── deleteFile / deleteFiles / renameFile ──

    describe('deleteFile', () => {
        it('calls delete API and reloads files', async () => {
            store.state.currentDir = '/project'
            store.state.currentFile = { path: '/project/old.txt', name: 'old.txt', content: '', isBinary: false, size: 0, isImage: false, isAudio: false }

            mockApiPost.mockResolvedValue({})

            await store.deleteFile('/project/old.txt')

            expect(mockApiPost).toHaveBeenCalledWith('/api/file/delete', { path: '/project/old.txt' })
            // currentFile should be cleared since it matches deleted path
            expect(store.state.currentFile).toBeNull()
        })

        it('does not clear currentFile if different file deleted', async () => {
            store.state.currentDir = '/project'
            store.state.currentFile = { path: '/project/other.txt', name: 'other.txt', content: '', isBinary: false, size: 0, isImage: false, isAudio: false }
            mockApiGet.mockResolvedValue({ entries: [] })

            mockApiPost.mockResolvedValue({})

            await store.deleteFile('/project/old.txt')

            expect(store.state.currentFile).not.toBeNull()
        })
    })

    describe('deleteFiles', () => {
        it('deletes multiple files and clears currentFile if matched', async () => {
            store.state.currentDir = '/project'
            store.state.currentFile = { path: '/project/a.txt', name: 'a.txt', content: '', isBinary: false, size: 0, isImage: false, isAudio: false }
            mockApiGet.mockResolvedValue({ entries: [] })

            mockApiPost.mockResolvedValue({})

            await store.deleteFiles(['/project/a.txt', '/project/b.txt'])

            expect(mockApiPost).toHaveBeenCalledTimes(2)
            expect(store.state.currentFile).toBeNull()
        })

        it('returns early when paths array is empty', async () => {
            await store.deleteFiles([])
            expect(mockApiPost).not.toHaveBeenCalled()
        })
    })

    describe('renameFile', () => {
        it('calls rename API and reloads files', async () => {
            store.state.currentDir = '/project'
            mockApiGet.mockResolvedValue({ entries: [] })
            mockApiPost.mockResolvedValue({})

            await store.renameFile('/project/old.txt', 'new.txt')

            expect(mockApiPost).toHaveBeenCalledWith('/api/file/rename', { path: '/project/old.txt', name: 'new.txt' })
        })

        it('shows error toast on rename failure', async () => {
            const err = Object.assign(new Error('rename failed'), { msgKey: 'InternalError' })
            mockApiPost.mockRejectedValue(err)
            mockToastShow.mockClear()

            await expect(store.renameFile('/project/old.txt', 'new.txt')).rejects.toThrow('rename failed')
            expect(mockToastShow).toHaveBeenCalled()
        })

        it('treats FileNotFoundShort as success', async () => {
            const err = Object.assign(new Error('not found'), { msgKey: 'FileNotFoundShort' })
            mockApiPost.mockRejectedValue(err)
            mockApiGet.mockResolvedValue({ entries: [] })
            mockToastShow.mockClear()

            await store.renameFile('/project/old.txt', 'new.txt')
            expect(mockToastShow).not.toHaveBeenCalled()
        })

        it('re-selects current file at new path after rename', async () => {
            store.state.currentDir = '/project'
            store.state.currentFile = { path: '/project/old.txt', name: 'old.txt' }
            mockApiPost.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ entries: [] })

            await store.renameFile('/project/old.txt', 'new.txt')

            // Should have called selectFile with the new path
            expect(mockApiGet).toHaveBeenCalled()
        })
    })

    // ── Directory navigation ──

    describe('navigateToDir', () => {
        it('calls loadFiles with the given path (normalized)', async () => {
            store.state.dirLoading = false
            mockApiGet.mockResolvedValue({ items: [] })

            await store.navigateToDir('/project/sub')

            // Leading slashes are stripped so the backend receives a relative path
            expect(mockApiGet).toHaveBeenCalledWith('/api/dir?path=project%2Fsub')
        })

        it('skips if dirLoading is true', async () => {
            store.state.dirLoading = true

            await store.navigateToDir('/project/sub')

            // loadFiles should not have been called (apiGet not called for dir)
            expect(mockApiGet).not.toHaveBeenCalled()
        })
    })

    describe('navigateToParentDir', () => {
        it('navigates to parent directory using dirName', async () => {
            store.state.dirLoading = false
            store.state.currentDir = 'src/composables'
            mockApiGet.mockResolvedValue({ items: [] })

            await store.navigateToParentDir()

            // dirName('src/composables') = 'src'
            expect(mockApiGet).toHaveBeenCalledWith('/api/dir?path=src')
        })

        it('skips if dirLoading is true', async () => {
            store.state.dirLoading = true
            store.state.currentDir = 'src/composables'

            await store.navigateToParentDir()

            expect(mockApiGet).not.toHaveBeenCalled()
        })

        it('navigates to root from one-level-deep directory', async () => {
            store.state.dirLoading = false
            store.state.currentDir = 'src'
            mockApiGet.mockResolvedValue({ items: [] })

            await store.navigateToParentDir()

            // dirName('src') = ''
            expect(mockApiGet).toHaveBeenCalledWith('/api/dir?path=')
        })

        it('no-op when already at project root', async () => {
            store.state.dirLoading = false
            store.state.currentDir = ''

            await store.navigateToParentDir()

            expect(mockApiGet).not.toHaveBeenCalled()
        })
    })

    // ── loadGitBranch ──

    describe('loadGitBranch', () => {
        it('updates git state from API', async () => {
            mockApiGet.mockResolvedValue({
                isGit: true,
                branch: 'feature/test',
                head: 'abc123def',
                dirty: true,
                changeCount: 5,
            })

            const result = await store.loadGitBranch()

            expect(store.state.gitBranch).toBe('feature/test')
            expect(store.state.gitHead).toBe('abc123def')
            expect(store.state.gitDirty).toBe(true)
            expect(store.state.gitWorkingTreeChangeCount).toBe(5)
            expect(result.isGit).toBe(true)
        })

        it('clears git state on API failure', async () => {
            store.state.gitBranch = 'main'
            store.state.gitHead = 'abc123'
            store.state.gitDirty = true
            store.state.gitWorkingTreeChangeCount = 3

            mockApiGet.mockRejectedValue(new Error('network error'))

            const result = await store.loadGitBranch()

            expect(store.state.gitBranch).toBe('')
            expect(store.state.gitHead).toBe('')
            expect(store.state.gitDirty).toBe(false)
            expect(store.state.gitWorkingTreeChangeCount).toBe(0)
            expect(result.isGit).toBe(false)
        })

        it('handles missing fields with defaults', async () => {
            mockApiGet.mockResolvedValue({})

            const result = await store.loadGitBranch()

            expect(store.state.gitBranch).toBe('')
            expect(store.state.gitHead).toBe('')
            expect(store.state.gitDirty).toBe(false)
            expect(store.state.gitWorkingTreeChangeCount).toBe(0)
        })
    })

    // ── loadProject: config fields from /api/roots ──

    describe('loadProject config fields', () => {
        it('reads uploadMaxSizeMB from roots API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'], uploadMaxSizeMB: 50 }
                if (url === '/api/project') return { path: '/home/user/project' }
                return {}
            })

            await store.loadProject()

            expect(store.state.uploadMaxSizeMB).toBe(50)
        })

        it('reads uploadMaxFiles from roots API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'], uploadMaxFiles: 5 }
                if (url === '/api/project') return { path: '/home/user/project' }
                return {}
            })

            await store.loadProject()

            expect(store.state.uploadMaxFiles).toBe(5)
        })

        it('reads chatInitialMessages from roots API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'], chatInitialMessages: 30 }
                if (url === '/api/project') return { path: '/home/user/project' }
                return {}
            })

            await store.loadProject()

            expect(store.state.chatInitialMessages).toBe(30)
        })

        it('reads chatPageSize from roots API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'], chatPageSize: 50 }
                if (url === '/api/project') return { path: '/home/user/project' }
                return {}
            })

            await store.loadProject()

            expect(store.state.chatPageSize).toBe(50)
        })

        it('reads chatSessionPageSize from roots API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'], chatSessionPageSize: 20 }
                if (url === '/api/project') return { path: '/home/user/project' }
                return {}
            })

            await store.loadProject()

            expect(store.state.chatSessionPageSize).toBe(20)
        })

        it('reads sessionMaxCount from roots API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'], sessionMaxCount: 50 }
                if (url === '/api/project') return { path: '/home/user/project' }
                return {}
            })

            await store.loadProject()

            expect(store.state.sessionMaxCount).toBe(50)
        })

        it('reads homeDir from /api/project', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'] }
                if (url === '/api/project') return { path: '/home/user/project', homeDir: '/home/user' }
                return {}
            })

            await store.loadProject()

            expect(store.state.homeDir).toBe('/home/user')
        })
    })

    // ── setProject: expanded response fields ──

    describe('setProject expanded response', () => {
        it('applies homeDir from expanded response', async () => {
            mockApiPost.mockResolvedValue({
                ok: 'ok',
                path: '/new/project',
                homeDir: '/home/user',
            })

            await store.setProject('/new/project')

            expect(store.state.homeDir).toBe('/home/user')
        })

        it('applies roots from expanded response', async () => {
            mockApiPost.mockResolvedValue({
                ok: 'ok',
                path: '/new/project',
                roots: ['/home/user', '/opt'],
            })

            await store.setProject('/new/project')

            expect(store.state.rootPaths).toEqual(['/home/user', '/opt'])
        })

        it('applies config fields from expanded response', async () => {
            mockApiPost.mockResolvedValue({
                ok: 'ok',
                path: '/new/project',
                uploadMaxSizeMB: 200,
                uploadMaxFiles: 10,
                chatInitialMessages: 15,
                chatPageSize: 30,
                chatSessionPageSize: 8,
                sessionMaxCount: 20,
                recentProjectsMaxCount: 5,
            })

            await store.setProject('/new/project')

            expect(store.state.uploadMaxSizeMB).toBe(200)
            expect(store.state.uploadMaxFiles).toBe(10)
            expect(store.state.chatInitialMessages).toBe(15)
            expect(store.state.chatPageSize).toBe(30)
            expect(store.state.chatSessionPageSize).toBe(8)
            expect(store.state.sessionMaxCount).toBe(20)
            expect(store.state.recentProjectsMaxCount).toBe(5)
        })

        it('does not apply config fields when values are 0 or missing', async () => {
            mockApiPost.mockResolvedValue({
                ok: 'ok',
                path: '/new/project',
                uploadMaxSizeMB: 0,
                uploadMaxFiles: 0,
            })

            await store.setProject('/new/project')

            // resetProjectState sets these to defaults, and 0 doesn't override
            expect(store.state.uploadMaxSizeMB).toBe(100)
            expect(store.state.uploadMaxFiles).toBe(20)
        })
    })

    // ── selectFile: HTML detection and relative paths ──

    describe('selectFile advanced', () => {
        it('detects HTML files for preview mode', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'page.html', path: 'page.html', content: '<html></html>' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('page.html')

            expect(store.state.currentFile?.isHtml).toBe(true)

            vi.unstubAllGlobals()
        })

        it('detects HTM files for preview mode', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'page.htm', path: 'page.htm', content: '<html></html>' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('page.htm')

            expect(store.state.currentFile?.isHtml).toBe(true)

            vi.unstubAllGlobals()
        })

        it('uses relative path URL for non-absolute paths', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: 'src/test.ts', content: 'hello' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('src/test.ts')

            expect(mockFetch).toHaveBeenCalledWith('/api/file/src%2Ftest.ts')

            vi.unstubAllGlobals()
        })

        it('uses relative path URL with forceText for non-absolute paths', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: 'src/test.ts', content: 'hello' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('src/test.ts', false, false, true, true)

            expect(mockFetch).toHaveBeenCalledWith('/api/file/src%2Ftest.ts?forceText=1')

            vi.unstubAllGlobals()
        })

        it('updates current file in-place when addToHistory is false and same path', async () => {
            store.state.currentFile = { name: 'test.ts', path: 'src/test.ts', content: 'old' }

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: 'src/test.ts', content: 'new' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('src/test.ts', false, false, false)

            // Should update in place (same object reference)
            expect(store.state.currentFile?.content).toBe('new')

            vi.unstubAllGlobals()
        })

        it('creates new file object when addToHistory is true', async () => {
            const originalFile = { name: 'test.ts', path: 'src/test.ts', content: 'old' }
            store.state.currentFile = originalFile

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: 'src/test.ts', content: 'new' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('src/test.ts', false, false, true)

            // Should be a new object
            expect(store.state.currentFile?.content).toBe('new')

            vi.unstubAllGlobals()
        })

        it('handles fetch network error', async () => {
            const mockFetch = vi.fn().mockRejectedValue(new Error('Network error'))
            vi.stubGlobal('fetch', mockFetch)

            const result = await store.selectFile('test.ts')

            expect(result).toBe(false)
            expect(store.state.fileLoading).toBe(false)

            vi.unstubAllGlobals()
        })
    })

    // ── loadFiles ──

    describe('loadFiles', () => {
        it('loads directory entries from API', async () => {
            mockApiGet.mockResolvedValue({
                items: [
                    { name: 'src', type: 'dir' },
                    { name: 'main.go', type: 'file' },
                ],
            })

            await store.loadFiles('/project')

            // Leading slashes are stripped: /project → project
            expect(store.state.currentDir).toBe('project')
            expect(store.state.dirEntries).toHaveLength(2)
            expect(store.state.dirLoading).toBe(false)
        })

        it('sets dirLoading to false after success', async () => {
            mockApiGet.mockResolvedValue({ items: [] })

            await store.loadFiles('')

            expect(store.state.dirLoading).toBe(false)
        })

        it('strips leading slashes from directory path', async () => {
            mockApiGet.mockResolvedValue({ items: [] })

            await store.loadFiles('///deeply/nested')

            expect(mockApiGet).toHaveBeenCalledWith('/api/dir?path=deeply%2Fnested')
            expect(store.state.currentDir).toBe('deeply/nested')
        })

        it('rolls back state on failure', async () => {
            store.state.currentDir = '/previous'
            store.state.dirEntries = [{ name: 'old', type: 'file' }]

            mockApiGet.mockRejectedValue(new Error('fail'))

            await store.loadFiles('/new')

            // Should roll back to previous state
            expect(store.state.currentDir).toBe('/previous')
            expect(store.state.dirEntries).toEqual([{ name: 'old', type: 'file' }])
            expect(store.state.dirLoading).toBe(false)
        })

        it('sets dirLoading to true while fetching', async () => {
            let resolveApi: (v: any) => void
            const apiPromise = new Promise(r => { resolveApi = r })
            mockApiGet.mockReturnValue(apiPromise)

            const loadPromise = store.loadFiles('/project')

            // While loading
            expect(store.state.dirLoading).toBe(true)

            resolveApi!({ items: [] })
            await loadPromise

            expect(store.state.dirLoading).toBe(false)
        })

        it('loads root directory when no path provided', async () => {
            mockApiGet.mockResolvedValue({ items: [{ name: 'home', type: 'dir' }] })

            await store.loadFiles('')

            expect(mockApiGet).toHaveBeenCalledWith('/api/dir?path=')
        })
    })

    // ── deleteFile error handling ──

    describe('deleteFile', () => {
        it('shows error toast on API failure', async () => {
            const err = Object.assign(new Error('delete failed'), { msgKey: 'InternalError' })
            mockApiPost.mockRejectedValue(err)
            mockApiGet.mockResolvedValue({ items: [] })

            await store.deleteFile('/project/test.txt')

            expect(mockToastShow).toHaveBeenCalledWith('file.toast.deleteFailed', { type: 'error', icon: '⚠️' })
            // loadFiles should still run even after error
            expect(mockApiGet).toHaveBeenCalled()
        })

        it('treats FileNotFoundShort as success (no toast)', async () => {
            const err = Object.assign(new Error('file not found'), { msgKey: 'FileNotFoundShort' })
            mockApiPost.mockRejectedValue(err)
            mockApiGet.mockResolvedValue({ items: [] })

            await store.deleteFile('/project/gone.txt')

            expect(mockToastShow).not.toHaveBeenCalled()
            // loadFiles should still refresh
            expect(mockApiGet).toHaveBeenCalled()
        })

        it('clears currentFile when deleting the viewed file', async () => {
            mockApiPost.mockResolvedValue({ ok: true })
            mockApiGet.mockResolvedValue({ items: [] })
            store.state.currentFile = { name: 'test.txt', path: '/project/test.txt' } as any

            await store.deleteFile('/project/test.txt')

            expect(store.state.currentFile).toBeNull()
        })

        it('does not delete when dialog is cancelled', async () => {
            mockDialogConfirm.mockResolvedValueOnce(false)

            await store.deleteFile('/project/test.txt')

            expect(mockApiPost).not.toHaveBeenCalled()
        })
    })

    // ── deleteFiles (batch) error handling ──

    describe('deleteFiles', () => {
        it('shows error toast when some deletes fail', async () => {
            mockApiPost
                .mockResolvedValueOnce({ ok: true })
                .mockRejectedValueOnce(Object.assign(new Error('failed'), { msgKey: 'InternalError' }))
            mockApiGet.mockResolvedValue({ items: [] })

            await store.deleteFiles(['/project/a.txt', '/project/b.txt'])

            expect(mockToastShow).toHaveBeenCalledWith('file.toast.deleteFailed', { type: 'error', icon: '⚠️' })
            // loadFiles should still run
            expect(mockApiGet).toHaveBeenCalled()
        })

        it('ignores FileNotFoundShort in batch delete', async () => {
            mockApiPost
                .mockResolvedValueOnce({ ok: true })
                .mockRejectedValueOnce(Object.assign(new Error('not found'), { msgKey: 'FileNotFoundShort' }))
            mockApiGet.mockResolvedValue({ items: [] })

            await store.deleteFiles(['/project/a.txt', '/project/gone.txt'])

            expect(mockToastShow).not.toHaveBeenCalled()
            expect(mockApiGet).toHaveBeenCalled()
        })

        it('refreshes file list even with partial failures', async () => {
            mockApiPost
                .mockRejectedValueOnce(Object.assign(new Error('fail'), { msgKey: 'InternalError' }))
                .mockResolvedValueOnce({ ok: true })
            mockApiGet.mockResolvedValue({ items: [] })

            await store.deleteFiles(['/project/a.txt', '/project/b.txt'])

            // loadFiles should be called despite partial failure
            expect(mockApiGet).toHaveBeenCalled()
        })
    })
})

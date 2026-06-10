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
    dirName: (p: string) => p.split('/').slice(0, -1).join('/') || '/',
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key,
}))

// Mock useToast
vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: vi.fn() }),
}))

// Mock useDialog
vi.mock('@/composables/useDialog', () => ({
    useDialog: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
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
            store.state.currentFile = { name: 'file.ts', path: '/file.ts' } as any

            store.resetProjectState()

            expect(store.state.currentDir).toBe('')
            expect(store.state.dirEntries).toEqual([])
            expect(store.state.dirLoading).toBe(false)
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
            store.state.chatUnread = true
            store.state.chatRunning = true
            store.state.taskUnread = true
            store.state.taskRunning = true
            store.state.taskJustCompleted = true
            store.state.tasks = [{ id: 'task-1' }]

            store.resetProjectState()

            expect(store.state.chatUnread).toBe(false)
            expect(store.state.chatRunning).toBe(false)
            expect(store.state.taskUnread).toBe(false)
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
            store.state.chatCollapsedHeight = 999
            store.state.sessionMaxCount = 999
            store.state.recentProjectsMaxCount = 999

            store.resetProjectState()

            expect(store.state.uploadMaxSizeMB).toBe(100)
            expect(store.state.uploadMaxFiles).toBe(20)
            expect(store.state.chatInitialMessages).toBe(20)
            expect(store.state.chatPageSize).toBe(20)
            expect(store.state.chatSessionPageSize).toBe(10)
            expect(store.state.chatCollapsedHeight).toBe(150)
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

        it('posts project path to recent-projects API', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/roots') return { roots: ['/'] }
                if (url === '/api/project') return { path: '/home/user/myproject' }
                return {}
            })
            mockApiPost.mockResolvedValue({})

            await store.loadProject()

            expect(mockApiPost).toHaveBeenCalledWith('/api/recent-projects', { path: '/home/user/myproject' })
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

            // URL should not contain double slash — cleanPath strips leading /
            expect(mockFetch).toHaveBeenCalledWith('/api/file/test.ts')
            vi.unstubAllGlobals()
        })

        it('strips multiple leading slashes from path', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'test.ts', path: '///test.ts', content: 'hello' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('///test.ts')

            expect(mockFetch).toHaveBeenCalledWith('/api/file/test.ts')
            vi.unstubAllGlobals()
        })

        it('uses forceText=1 query param when forceText is true for non-text file', async () => {
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ name: 'file.bin', path: '/file.bin', content: 'data' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            await store.selectFile('/file.bin', false, false, true, true)

            expect(mockFetch).toHaveBeenCalledWith('/api/file/file.bin?forceText=1')
            vi.unstubAllGlobals()
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
            // After setProject, resetProjectState should have been called
            expect(store.state.projectRoot).toBe('')
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
})

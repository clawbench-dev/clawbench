import { describe, expect, it } from 'vitest'
import { writeFileToTree } from '@/utils/dirHandle.ts'

interface FakeWritable {
  writes: unknown[]
  closed: boolean
  write: (data: unknown) => Promise<void>
  close: () => Promise<void>
}

interface FakeFileHandle {
  writes: unknown[]
  createWritable: () => Promise<FakeWritable>
}

interface FakeDirHandle {
  dirs: Map<string, FakeDirHandle>
  files: Map<string, FakeFileHandle>
  getDirectoryHandle: (name: string, opts?: { create?: boolean }) => Promise<FakeDirHandle>
  getFileHandle: (name: string, opts?: { create?: boolean }) => Promise<FakeFileHandle>
}

function fakeDirHandle(
  dirs = new Map<string, FakeDirHandle>(),
  files = new Map<string, FakeFileHandle>(),
): FakeDirHandle {
  const dir: FakeDirHandle = {
    dirs,
    files,
    getDirectoryHandle: async (name, opts) => {
      if (!dirs.has(name)) {
        if (!opts?.create) throw new Error('dir not found')
        dirs.set(name, fakeDirHandle())
      }
      return dirs.get(name)!
    },
    getFileHandle: async (name, opts) => {
      if (!files.has(name)) {
        if (!opts?.create) throw new Error('file not found')
        files.set(name, {
          writes: [],
          createWritable: async () => {
            const fh = files.get(name)!
            const w: FakeWritable = {
              writes: fh.writes,
              closed: false,
              write: async (data) => {
                fh.writes.push(data)
              },
              close: async () => {
                w.closed = true
              },
            }
            return w
          },
        })
      }
      return files.get(name)!
    },
  }
  return dir
}

describe('writeFileToTree', () => {
  it('writes a file into the root directory', async () => {
    const root = fakeDirHandle()
    const blob = new Blob(['hello'])

    await writeFileToTree(root as unknown as FileSystemDirectoryHandle, 'a.txt', blob)

    const fh = root.files.get('a.txt')
    expect(fh).toBeDefined()
    expect(fh!.writes).toEqual([blob])
  })

  it('recreates nested directories and writes into the leaf', async () => {
    const root = fakeDirHandle()
    const blob = new Blob(['x'])

    await writeFileToTree(root as unknown as FileSystemDirectoryHandle, 'src/utils/helper.ts', blob)

    expect(root.dirs.has('src')).toBe(true)
    expect(root.dirs.get('src')!.dirs.has('utils')).toBe(true)
    expect(root.dirs.get('src')!.dirs.get('utils')!.files.has('helper.ts')).toBe(true)
  })

  it('handles a top-level file with no subdirectories', async () => {
    const root = fakeDirHandle()

    await writeFileToTree(root as unknown as FileSystemDirectoryHandle, 'readme.md', new Blob(['r']))

    expect(root.files.has('readme.md')).toBe(true)
    expect(root.dirs.size).toBe(0)
  })
})

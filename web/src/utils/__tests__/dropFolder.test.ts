import { describe, expect, it } from 'vitest'
import { expandDataTransfer, type DropFile, type ExpandResult } from '@/utils/dropFolder.ts'

// ── Fake FileSystemEntry helpers (jsdom has no webkitGetAsEntry) ──

interface FakeEntry {
  isFile: boolean
  isDirectory: boolean
  name: string
  fullPath: string
  file?: (cb: (f: File) => void) => void
  createReader?: () => { readEntries: (cb: (e: FakeEntry[]) => void) => void }
}

function makeFileEntry(fullPath: string, content = 'x'): FakeEntry {
  const name = fullPath.split('/').pop() || 'file'
  return {
    isFile: true,
    isDirectory: false,
    name,
    fullPath,
    file: (cb) => cb(new File([content], name, { type: 'text/plain' })),
  }
}

function makeDirEntry(name: string, fullPath: string, children: FakeEntry[]): FakeEntry {
  let done = false
  return {
    isFile: false,
    isDirectory: true,
    name,
    fullPath,
    createReader: () => ({
      readEntries: (cb) => {
        if (done) {
          cb([])
          return
        }
        done = true
        cb(children)
      },
    }),
  }
}

function dataTransferWith(items: Array<{ getEntry: () => FakeEntry | null }>): DataTransfer {
  return {
    items: items.map((it) => ({ webkitGetAsEntry: it.getEntry })),
    files: [] as unknown as FileList,
  } as unknown as DataTransfer
}

describe('expandDataTransfer (webkitGetAsEntry traversal)', () => {
  it('expands a dropped folder into files with their relative directory path', async () => {
    const root = makeDirEntry('新建文件夹', '/新建文件夹', [
      makeDirEntry('src', '/新建文件夹/src', [makeFileEntry('/新建文件夹/src/a.ts', 'A')]),
      makeFileEntry('/新建文件夹/README.md', 'R'),
    ])
    const result: ExpandResult = await expandDataTransfer(dataTransferWith([{ getEntry: () => root }]))
    expect(result.files).toHaveLength(2)
    expect(result.files.find((f) => f.file.name === 'a.ts')?.relPath).toBe('新建文件夹/src')
    expect(result.files.find((f) => f.file.name === 'README.md')?.relPath).toBe('新建文件夹')
    expect(result.emptyDirs).toEqual([])
  })

  it('detects empty directories and records them', async () => {
    const root = makeDirEntry('proj', '/proj', [
      makeDirEntry('empty', '/proj/empty', []),
      makeFileEntry('/proj/main.go', 'M'),
    ])
    const result = await expandDataTransfer(dataTransferWith([{ getEntry: () => root }]))
    expect(result.emptyDirs).toEqual(['proj/empty'])
    expect(result.files.map((f) => f.file.name)).toEqual(['main.go'])
  })

  it('treats a loose dropped file as flat (empty relPath)', async () => {
    const result = await expandDataTransfer(
      dataTransferWith([{ getEntry: () => makeFileEntry('/loose.txt', 'L') }]),
    )
    expect(result.files).toHaveLength(1)
    expect(result.files[0].relPath).toBe('')
  })

  it('returns empty result for empty input', async () => {
    const result = await expandDataTransfer({ items: [], files: [] as unknown as FileList } as unknown as DataTransfer)
    expect(result.files).toEqual([])
    expect(result.emptyDirs).toEqual([])
  })

  it('falls back to dataTransfer.files when webkitGetAsEntry is unavailable', async () => {
    const f = new File(['x'], 'flat.txt', { type: 'text/plain' })
    const dt = { items: [], files: [f] } as unknown as DataTransfer
    const result = await expandDataTransfer(dt)
    expect(result.files).toHaveLength(1)
    expect(result.files[0].file.name).toBe('flat.txt')
  })

  it('keeps multiple dropped files from a folder', async () => {
    const root = makeDirEntry('mydir', '/mydir', [
      makeFileEntry('/mydir/one.js', '1'),
      makeFileEntry('/mydir/two.js', '2'),
    ])
    const result = await expandDataTransfer(dataTransferWith([{ getEntry: () => root }]))
    expect(result.files).toHaveLength(2)
    for (const f of result.files) expect(f.relPath).toBe('mydir')
  })

  it('collapses multiple empty dirs into emptyDirs list', async () => {
    const root = makeDirEntry('root', '/root', [
      makeDirEntry('a', '/root/a', []),
      makeDirEntry('b', '/root/b', []),
    ])
    const result = await expandDataTransfer(dataTransferWith([{ getEntry: () => root }]))
    expect(result.emptyDirs.sort()).toEqual(['root/a', 'root/b'])
  })
})

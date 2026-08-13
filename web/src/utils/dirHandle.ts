/**
 * Write a blob into a directory tree rooted at `root`, recreating any missing
 * subdirectories along the way. `relPath` is slash-separated (e.g. "a/b/c.txt").
 *
 * Used by the "download as directory tree" feature (File System Access API):
 * the client picks a target directory, then writes each downloaded file back
 * under the same relative path to reconstruct the original tree locally.
 */
export async function writeFileToTree(
    root: FileSystemDirectoryHandle,
    relPath: string,
    blob: Blob,
): Promise<void> {
    const parts = relPath.split('/').filter(Boolean)
    const fileName = parts.pop()
    if (!fileName) return

    let dir: FileSystemDirectoryHandle = root
    for (const part of parts) {
        dir = await dir.getDirectoryHandle(part, { create: true })
    }

    const fileHandle = await dir.getFileHandle(fileName, { create: true })
    const writable = await fileHandle.createWritable()
    await writable.write(blob)
    await writable.close()
}

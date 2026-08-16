import { describe, it, expect, vi } from 'vitest'
import { retryableImport } from '../lazyMermaid.ts'

describe('retryableImport', () => {
    it('resolves on the first successful attempt without retrying', async () => {
        const loader = vi.fn().mockResolvedValue('mod')
        await expect(retryableImport(loader)).resolves.toBe('mod')
        expect(loader).toHaveBeenCalledTimes(1)
    })

    it('retries and succeeds when an earlier attempt fails', async () => {
        const loader = vi.fn()
            .mockRejectedValueOnce(new Error('Failed to fetch dynamically imported module'))
            .mockRejectedValueOnce(new Error('network hiccup'))
            .mockResolvedValueOnce('mod')
        await expect(retryableImport(loader)).resolves.toBe('mod')
        expect(loader).toHaveBeenCalledTimes(3)
    })

    it('throws the last error when all attempts fail', async () => {
        const loader = vi.fn().mockRejectedValue(new Error('Failed to fetch dynamically imported module'))
        await expect(retryableImport(loader, 3)).rejects.toThrow('Failed to fetch dynamically imported module')
        expect(loader).toHaveBeenCalledTimes(3)
    })
})

import { beforeEach, describe, expect, it } from 'vitest'
import {
  consumeKBPendingOpen,
  kbPendingOpen,
  requestOpenKBDocument,
} from '../kbNavigation'

describe('kbNavigation', () => {
  beforeEach(() => {
    kbPendingOpen.value = null
  })

  it('parks the latest request and consumes it once', () => {
    requestOpenKBDocument(7, 31)
    expect(kbPendingOpen.value).toMatchObject({ documentId: 7, chunkId: 31 })
    const token = kbPendingOpen.value!.token

    // A stale token (an older panel instance) must not clear the new request.
    consumeKBPendingOpen(token - 1)
    expect(kbPendingOpen.value).not.toBeNull()

    consumeKBPendingOpen(token)
    expect(kbPendingOpen.value).toBeNull()
  })

  it('treats a missing chunk id as "open at the top"', () => {
    requestOpenKBDocument(9)
    expect(kbPendingOpen.value).toMatchObject({ documentId: 9, chunkId: 0 })
  })

  it('ignores a zero document id', () => {
    requestOpenKBDocument(0)
    expect(kbPendingOpen.value).toBeNull()
  })
})

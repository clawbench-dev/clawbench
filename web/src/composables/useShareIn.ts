import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

interface ShareInFile {
  name: string
  path: string
  size: number
  modTime: string
}

const recentShares = ref<ShareInFile[]>([])
let fetched = false

async function fetchRecentShares() {
  try {
    const res = await fetch('/api/share-in/recent')
    if (res.ok) {
      recentShares.value = await res.json()
      fetched = true
    }
  } catch (e) {
    appLog.w('ShareIn', 'Failed to fetch recent shares', e)
  }
}

/** Ensure recent shares are fetched once. Call when attach menu opens. */
async function ensureRecentShares() {
  if (!fetched) await fetchRecentShares()
}

/** Refresh recent shares (call after a new share-in upload). */
async function refreshRecentShares() {
  fetched = false
  await fetchRecentShares()
}

export function useShareIn() {
  return { recentShares, fetchRecentShares: ensureRecentShares, refreshRecentShares }
}

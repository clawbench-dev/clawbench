import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

interface ShareInFile {
  name: string
  path: string
  size: number
  modTime: string
}

const recentShares = ref<ShareInFile[]>([])

async function fetchRecentShares() {
  try {
    const res = await fetch('/api/share-in/recent')
    if (res.ok) {
      recentShares.value = await res.json()
    }
  } catch (e) {
    appLog.w('ShareIn', 'Failed to fetch recent shares', e)
  }
}

export function useShareIn() {
  return { recentShares, fetchRecentShares }
}

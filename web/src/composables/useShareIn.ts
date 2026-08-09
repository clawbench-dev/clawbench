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

// Delete a recently shared file and remove it from the local list.
async function deleteRecentShare(path: string) {
  try {
    const res = await fetch('/api/share-in/recent', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    })
    if (res.ok) {
      recentShares.value = recentShares.value.filter(f => f.path !== path)
      return true
    }
    appLog.w('ShareIn', 'Failed to delete recent share', path, res.status)
  } catch (e) {
    appLog.w('ShareIn', 'Failed to delete recent share', e)
  }
  return false
}

export function useShareIn() {
  return { recentShares, fetchRecentShares, deleteRecentShare }
}

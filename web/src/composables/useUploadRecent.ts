import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

interface UploadFile {
  name: string
  path: string
  size: number
  modTime: string
}

const recentUploads = ref<UploadFile[]>([])

async function fetchRecentUploads() {
  try {
    const res = await fetch('/api/upload/recent')
    if (res.ok) {
      recentUploads.value = await res.json()
    }
  } catch (e) {
    appLog.w('UploadRecent', 'Failed to fetch recent uploads', e)
  }
}

// Delete a recently uploaded file and remove it from the local list.
async function deleteRecentUpload(path: string) {
  try {
    const res = await fetch('/api/upload/recent', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    })
    if (res.ok) {
      recentUploads.value = recentUploads.value.filter(f => f.path !== path)
      return true
    }
    appLog.w('UploadRecent', 'Failed to delete recent upload', path, res.status)
  } catch (e) {
    appLog.w('UploadRecent', 'Failed to delete recent upload', e)
  }
  return false
}

export function useUploadRecent() {
  return { recentUploads, fetchRecentUploads, deleteRecentUpload }
}

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
      const data = await res.json()
      // Normalize to an array. A nil slice on the backend is encoded as `null`
      // when the (existing but empty) uploads dir has no files; assigning null
      // here would crash the AttachDrawer Uploads tab on `recentUploads.length`.
      recentUploads.value = Array.isArray(data) ? data : []
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
    // Only trust the delete when the server confirms it with {ok:true}. An old
    // server without the DELETE handler returns 200 + a JSON array, which we
    // must NOT treat as success — otherwise the file is only hidden, not deleted.
    if (res.ok) {
      const data = await res.json().catch(() => null)
      if (data && data.ok === true) {
        recentUploads.value = recentUploads.value.filter(f => f.path !== path)
        return true
      }
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

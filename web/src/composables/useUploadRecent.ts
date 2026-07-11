import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

interface UploadFile {
  name: string
  path: string
  size: number
  modTime: string
}

const recentUploads = ref<UploadFile[]>([])
let fetched = false

async function fetchRecentUploads() {
  try {
    const res = await fetch('/api/upload/recent')
    if (res.ok) {
      recentUploads.value = await res.json()
      fetched = true
    }
  } catch (e) {
    appLog.w('UploadRecent', 'Failed to fetch recent uploads', e)
  }
}

/** Ensure recent uploads are fetched once. */
async function ensureRecentUploads() {
  if (!fetched) await fetchRecentUploads()
}

/** Refresh recent uploads (call after a new upload). */
async function refreshRecentUploads() {
  fetched = false
  await fetchRecentUploads()
}

export function useUploadRecent() {
  return { recentUploads, fetchRecentUploads: ensureRecentUploads, refreshRecentUploads }
}

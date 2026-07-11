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

export function useUploadRecent() {
  return { recentUploads, fetchRecentUploads }
}

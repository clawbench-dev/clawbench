import { ref, type Ref } from 'vue'
import { apiGet, apiPost, apiPut, apiDelete } from '@/utils/api'

/** Base shape every CRUD list item must satisfy */
export interface CrudItem {
  id: number
  label: string
  sort_order?: number
}

export interface UseCrudListOptions {
  /** API endpoint prefix, e.g. '/api/chat/quick-send' */
  apiPrefix: string
  /** Human-readable name used only in error messages */
  itemName?: string
}

type GenericInstance = {
  items: Ref<unknown[]>
  loaded: ReturnType<typeof useCrudList>['loaded']
  showEditDialog: ReturnType<typeof useCrudList>['showEditDialog']
  fetchItems: (force?: boolean) => Promise<void>
  addItem: (item: Record<string, unknown>) => Promise<boolean>
  updateItem: (id: number, item: Record<string, unknown>) => Promise<boolean>
  deleteItem: (id: number) => Promise<boolean>
  reorderItems: (ids: number[]) => Promise<boolean>
}

/** Per-apiPrefix state bucket — each prefix gets its own independent refs */
interface StateBucket {
  items: Ref<unknown[]>
  loaded: Ref<boolean>
  showEditDialog: Ref<boolean>
}

const _singletons = new Map<string, GenericInstance>()
const _state = new Map<string, StateBucket>()

function getBucket(key: string): StateBucket {
  if (!_state.has(key)) {
    _state.set(key, {
      items: ref<unknown[]>([]),
      loaded: ref(false),
      showEditDialog: ref(false),
    })
  }
  return _state.get(key)!
}

async function _fetchItems(key: string, force = false) {
  const bucket = getBucket(key)
  if (bucket.loaded.value && !force) return
  try {
    bucket.items.value = (await apiGet<unknown[]>(key)) || []
    bucket.loaded.value = true
  } catch {
    // Silently fail on initial load
  }
}

async function _addItem(key: string, item: Record<string, unknown>): Promise<boolean> {
  try {
    await apiPost(key, item)
    await _fetchItems(key, true)
    return true
  } catch {
    return false
  }
}

async function _updateItem(
  key: string,
  id: number,
  item: Record<string, unknown>
): Promise<boolean> {
  try {
    await apiPut(`${key}/${id}`, item)
    await _fetchItems(key, true)
    return true
  } catch {
    return false
  }
}

async function _deleteItem(key: string, id: number): Promise<boolean> {
  try {
    await apiDelete(`${key}/${id}`)
    await _fetchItems(key, true)
    return true
  } catch {
    return false
  }
}

async function _reorderItems(key: string, ids: number[]): Promise<boolean> {
  const bucket = getBucket(key)
  const oldItems = [...bucket.items.value] as Record<string, unknown>[]
  // Optimistic reorder
  const reordered = ids
    .map((id, i) => {
      const item = (bucket.items.value as Record<string, unknown>[]).find(it => it['id'] === id)
      return item ? { ...item, sort_order: i } : null
    })
    .filter(Boolean) as Record<string, unknown>[]
  bucket.items.value = reordered
  try {
    await apiPut(`${key}/reorder`, { ids })
    return true
  } catch {
    bucket.items.value = oldItems // Rollback
    return false
  }
}

/** @internal Reset all singleton state — for tests only */
export function _resetAllForTesting() {
  _singletons.clear()
  _state.clear()
}

export function useCrudList<T extends CrudItem>(options: UseCrudListOptions) {
  const key = options.apiPrefix

  if (!_singletons.has(key)) {
    const bucket = getBucket(key)
    _singletons.set(key, {
      items: bucket.items as ReturnType<typeof useCrudList>['items'],
      loaded: bucket.loaded,
      showEditDialog: bucket.showEditDialog,
      fetchItems: (force?: boolean) => _fetchItems(key, force),
      addItem: (item: Record<string, unknown>) => _addItem(key, item),
      updateItem: (id: number, item: Record<string, unknown>) => _updateItem(key, id, item),
      deleteItem: (id: number) => _deleteItem(key, id),
      reorderItems: (ids: number[]) => _reorderItems(key, ids),
    })
  }

  return _singletons.get(key)! as {
    items: Ref<T[]>
    loaded: Ref<boolean>
    showEditDialog: Ref<boolean>
    fetchItems: (force?: boolean) => Promise<void>
    addItem: (item: Omit<T, 'id' | 'sort_order'>) => Promise<boolean>
    updateItem: (id: number, item: Partial<T>) => Promise<boolean>
    deleteItem: (id: number) => Promise<boolean>
    reorderItems: (ids: number[]) => Promise<boolean>
  }
}

import { useCrudList } from './useCrudList'

export interface QuickSendItem {
  id: number
  label: string
  command: string
  sort_order: number
  project_only: boolean
}

export function useQuickSend() {
  const { items, loaded, showEditDialog, fetchItems, addItem, updateItem, deleteItem, reorderItems } =
    useCrudList<QuickSendItem>({
      apiPrefix: '/api/chat/quick-send',
    })

  return {
    items: items as ReturnType<typeof useCrudList<QuickSendItem>>['items'],
    loaded,
    showEditDialog,
    fetchItems,
    addItem,
    updateItem,
    deleteItem,
    reorderItems,
  }
}

import { computed } from 'vue'
import { useCrudList } from './useCrudList'
import type { CrudItem } from './useCrudList'

export interface QuickCommand extends CrudItem {
  id: number
  label: string
  command: string
  hidden: boolean
  auto_execute: boolean
  sort_order: number
  project_only: boolean
}

export function useQuickCommands() {
  const { items, loaded, showEditDialog, fetchItems, addItem, updateItem, deleteItem, reorderItems } =
    useCrudList<QuickCommand>({
      apiPrefix: '/api/terminal/quick-commands',
    })

  const commands = items as ReturnType<typeof useCrudList<QuickCommand>>['items']
  const visibleCommands = computed(() => (commands.value as QuickCommand[]).filter(c => !c.hidden))
  // Prefer a project-scoped auto-execute command over a global one when both exist.
  const autoExecCommand = computed(
    () =>
      (commands.value as QuickCommand[]).find(c => c.auto_execute && c.project_only) ||
      (commands.value as QuickCommand[]).find(c => c.auto_execute) ||
      null
  )

  async function fetchCommands(force = false) {
    await fetchItems(force)
  }

  async function addCommand(cmd: Omit<QuickCommand, 'id' | 'sort_order'>) {
    return addItem(cmd)
  }

  async function updateCommand(id: number, cmd: Partial<Omit<QuickCommand, 'id'>>) {
    return updateItem(id, cmd)
  }

  async function deleteCommand(id: number) {
    return deleteItem(id)
  }

  async function reorderCommands(ids: number[]) {
    return reorderItems(ids)
  }

  return {
    commands,
    visibleCommands,
    autoExecCommand,
    fetchCommands,
    addCommand,
    updateCommand,
    deleteCommand,
    reorderCommands,
    showEditDialog,
    loaded,
  }
}

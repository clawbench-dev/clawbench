import { ref, computed, watch, type Ref } from 'vue'
import { listGroupMembers, type GroupMemberInfo } from '@/composables/useGroupChat'
import { getAgentAvatar, useAgents } from '@/composables/useAgents'

/**
 * Loads and caches the member roster of the CURRENT group session, and exposes
 * helpers to resolve a message's speaker (by member row id) and identify the
 * host. Outside a group the roster is empty and resolveSpeaker returns null.
 */
export function useGroupMembers(currentSessionId: Ref<string>) {
  const members = ref<GroupMemberInfo[]>([])
  const { loadAgents } = useAgents()

  const hostMemberId = computed(() => members.value.find(m => m.isHost)?.id || '')

  const byId = computed(() => {
    const map = new Map<string, GroupMemberInfo>()
    for (const m of members.value) map.set(m.id, m)
    return map
  })

  /** resolveSpeaker maps a member row id to { name, backend, avatar } or null. */
  function resolveSpeaker(memberRowId: string): { name: string; backend: string; avatar: string } | null {
    const m = byId.value.get(memberRowId)
    if (!m) return null
    // Avatar is a property of the underlying AGENT, not the member row: resolve
    // it from the member's agentId so a custom avatar set on the agent shows.
    return { name: m.name, backend: m.backend, avatar: getAgentAvatar(m.agentId) || '' }
  }

  async function refresh(sessionId: string) {
    if (!sessionId) {
      members.value = []
      return
    }
    // Agents carry the custom avatars the member bar/speaker header render, so
    // make sure the roster is loaded before/alongside the members.
    void loadAgents()
    try {
      members.value = await listGroupMembers(sessionId)
    } catch {
      members.value = []
    }
  }

  watch(currentSessionId, (sid) => { void refresh(sid) }, { immediate: true })

  return { members, hostMemberId, resolveSpeaker, refresh }
}

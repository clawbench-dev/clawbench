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
  const mode = ref<'host' | 'free'>('host')
  /** Free-mode "并发执行" switch (server-authoritative). */
  const parallelDefault = ref(false)
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

  /** resolveByName maps a display name to { name, backend, avatar } or null.
   *  Used to render @-mention chips for the host's routing targets, which are
   *  named (not id'd) in the routing tag. */
  function resolveByName(name: string): { name: string; backend: string; avatar: string } | null {
    const target = name.trim()
    if (!target) return null
    const m = members.value.find(x => x.name.trim() === target)
    if (!m) return null
    return { name: m.name, backend: m.backend, avatar: getAgentAvatar(m.agentId) || '' }
  }

  async function refresh(sessionId: string) {
    if (!sessionId) {
      members.value = []
      mode.value = 'host'
      parallelDefault.value = false
      return
    }
    // Agents carry the custom avatars the member bar/speaker header render, so
    // make sure the roster is loaded before/alongside the members.
    void loadAgents()
    try {
      const res = await listGroupMembers(sessionId)
      members.value = res.members
      mode.value = res.mode
      parallelDefault.value = res.parallelDefault
    } catch {
      members.value = []
      mode.value = 'host'
      parallelDefault.value = false
    }
  }

  watch(currentSessionId, (sid) => { void refresh(sid) }, { immediate: true })

  return { members, mode, parallelDefault, hostMemberId, resolveSpeaker, resolveByName, refresh }
}

import { ref, computed, watch, type Ref } from 'vue'
import { listGroupMembers, type GroupMemberInfo } from '@/composables/useGroupChat'
import { getAgentAvatar, useAgents } from '@/composables/useAgents'

/** GroupSpeakerIdentity is the resolved shape a message speaker renders with. */
export interface GroupSpeakerIdentity {
  name: string
  backend: string
  avatar: string
  agentName: string
  /** The underlying real agent id (GroupMemberInfo.agentId). Lets the speaker
   *  header open that agent's config page — the message's own `agentId` is the
   *  member ROW id, which no config page can resolve. */
  agentId: string
}

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
  /** Auto-cruise switch (server-authoritative, design §15). */
  const cruise = ref(false)
  const { loadAgents } = useAgents()

  const hostMemberId = computed(() => members.value.find(m => m.isHost)?.id || '')

  const byId = computed(() => {
    const map = new Map<string, GroupMemberInfo>()
    for (const m of members.value) map.set(m.id, m)
    return map
  })

  function toIdentity(m: GroupMemberInfo): GroupSpeakerIdentity {
    // 成员自己的头像快照优先；旧成员无快照时回退智能体头像。
    return { name: m.name, backend: m.backend, avatar: m.avatar || getAgentAvatar(m.agentId) || '', agentName: m.agentName, agentId: m.agentId }
  }

  /** resolveSpeaker maps a member row id to a speaker identity or null. */
  function resolveSpeaker(memberRowId: string): GroupSpeakerIdentity | null {
    const m = byId.value.get(memberRowId)
    if (!m) return null
    return toIdentity(m)
  }

  /** resolveByName maps a display name to a speaker identity or null.
   *  Used to render @-mention chips for the host's routing targets, which are
   *  named (not id'd) in the routing tag. */
  function resolveByName(name: string): GroupSpeakerIdentity | null {
    const target = name.trim()
    if (!target) return null
    const m = members.value.find(x => x.name.trim() === target)
    if (!m) return null
    return toIdentity(m)
  }

  async function refresh(sessionId: string) {
    if (!sessionId) {
      members.value = []
      mode.value = 'host'
      parallelDefault.value = false
      cruise.value = false
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
      cruise.value = res.cruise
    } catch {
      members.value = []
      mode.value = 'host'
      parallelDefault.value = false
      cruise.value = false
    }
  }

  watch(currentSessionId, (sid) => { void refresh(sid) }, { immediate: true })

  return { members, mode, parallelDefault, cruise, hostMemberId, resolveSpeaker, resolveByName, refresh }
}

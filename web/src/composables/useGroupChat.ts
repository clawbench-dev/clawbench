import { appLog } from '@/utils/appLog'

/**
 * Frontend API client for AI group chat. Mirrors the endpoints registered in
 * internal/handler/group.go.
 */

export interface GroupMemberInfo {
  id: string
  agentId: string
  name: string
  backend: string
  /** 成员自身的头像 SVG（agent_avatar）；空 = 回退到智能体头像。 */
  avatar: string
  /** 底层真实智能体名；与 name 相同时也带回，供 UI 判定是否披露。 */
  agentName: string
  left: boolean
  isHost: boolean
}

export interface CreateGroupResult {
  groupId: string
  hostMemberId: string
  /** "host" (a host routes) or "free" (no host, @-relay). */
  mode: 'host' | 'free'
}

async function postJSON(path: string, body: unknown): Promise<Record<string, unknown>> {
  const resp = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!resp.ok) {
    appLog.w('Group', `POST ${path} failed: ${resp.status}`)
    throw new Error(`request failed: ${resp.status}`)
  }
  return resp.json()
}

/** createGroup creates a group together with ALL its members in one call
 *  (design §7.1, decision #25). When hostAgentId is empty the group is created
 *  in FREE mode (no host; participants relay via @-mentions) — see design §13.
 *  When present, hostAgentId must be in memberAgentIds and the group is HOST
 *  mode. The backend creates the group and every member atomically. */
export async function createGroup(hostAgentId: string, memberAgentIds: string[]): Promise<CreateGroupResult> {
  const data = await postJSON('/api/group/create', { hostAgentId, memberAgentIds })
  return {
    groupId: String(data.groupId ?? ''),
    hostMemberId: String(data.hostMemberId ?? ''),
    mode: data.mode === 'free' ? 'free' : 'host',
  }
}

/** listGroupMembers fetches the group roster (including left members) plus the
 *  group's mode ("host" | "free") and its parallelDefault (the free-mode
 *  action-bar "并发执行" switch's server value). The member-speech cap is a
 *  global setting (chat.group_max_speeches), not a per-group one, so it is not
 *  returned here. */
export async function listGroupMembers(groupId: string): Promise<{ members: GroupMemberInfo[]; mode: 'host' | 'free'; parallelDefault: boolean }> {
  const resp = await fetch(`/api/group/members?groupId=${encodeURIComponent(groupId)}`)
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
  const data = await resp.json()
  return {
    members: data.members ?? [],
    mode: data.mode === 'free' ? 'free' : 'host',
    parallelDefault: data.parallelDefault === true,
  }
}

/** addGroupMembers adds one or more agents to the group. */
export async function addGroupMembers(groupId: string, agentIds: string[]): Promise<string[]> {
  const data = await postJSON('/api/group/members', { groupId, agentIds })
  return (data.memberIds as string[]) ?? []
}

/** setMemberAvatars 批量写回成员头像（DiceBear 生成，落库以对齐分享/导出）。 */
export async function setMemberAvatars(groupId: string, avatars: { memberId: string; avatar: string }[]): Promise<void> {
  await postJSON('/api/group/member-avatars', { groupId, avatars })
}

/**
 * Generate + persist avatars for the group members that need their OWN one.
 *
 * Only a REPEATED agent gets generated avatars: when the same agent is added to
 * the group more than once, every instance past the first is given a DiceBear
 * avatar (seeded by its unique name) so the copies are visually distinct. A
 * LONE agent keeps the agent's own configured avatar — there is nothing to tell
 * apart, so no generated avatar is written (the icon falls back to
 * getAgentAvatar).
 *
 * "First instance" is decided over ACTIVE members in roster order (which is
 * creation order): a left (archived) member does not count, since the group
 * shows a single active copy. A member that already has an avatar is never
 * re-generated (idempotent, so a partial failure can be retried by a later add).
 *
 * Called right after a create/add. onlyMemberIds, when given, scopes the
 * backfill to the freshly added members; the first-instance decision still sees
 * the whole roster, so a newly added SECOND instance is generated while a
 * pre-existing first is left on the agent avatar.
 *
 * The DiceBear runtime is imported lazily so the generator chunk never enters
 * the main bundle and is only fetched on the (rare) create/add path.
 */
export async function generateMemberAvatars(groupId: string, onlyMemberIds?: string[]): Promise<void> {
  const { members } = await listGroupMembers(groupId)
  const seenAgents = new Set<string>()
  const targets = members.filter(m => {
    if (m.left) return false
    // Roster order is creation order, so the first ACTIVE member of an agent is
    // its first instance; it keeps the agent avatar. Later instances are the
    // repeated copies that need their own generated avatar.
    const isFirstInstance = !seenAgents.has(m.agentId)
    seenAgents.add(m.agentId)
    if (isFirstInstance) return false
    if (m.avatar) return false
    return !onlyMemberIds || onlyMemberIds.includes(m.id)
  })
  if (!targets.length) return
  const { generateAvatarSvg } = await import('@/utils/lazyAvatar')
  const avatars = await Promise.all(targets.map(async m => ({ memberId: m.id, avatar: await generateAvatarSvg(m.name) })))
  await setMemberAvatars(groupId, avatars)
}

/** removeGroupMember soft-removes a member (keeps their past speech). */
export async function removeGroupMember(groupId: string, memberId: string): Promise<void> {
  const resp = await fetch('/api/group/members', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, memberId }),
  })
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}

/** setGroupParallelDefault updates the free-mode "并发执行" switch: when on, the
 *  user's @-mentions run concurrently. It is the group's only remaining
 *  per-group setting; the member-speech cap is global (chat.group_max_speeches). */
export async function setGroupParallelDefault(groupId: string, enabled: boolean): Promise<void> {
  const resp = await fetch('/api/group/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, parallelDefault: enabled }),
  })
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}

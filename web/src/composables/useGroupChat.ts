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
 *  group's current maxRounds (so the settings UI shows the server value rather
 *  than a hardcoded default), its mode ("host" | "free"), and its
 *  parallelDefault (the free-mode action-bar "并发执行" switch's server value). */
export async function listGroupMembers(groupId: string): Promise<{ members: GroupMemberInfo[]; maxRounds: number; mode: 'host' | 'free'; parallelDefault: boolean }> {
  const resp = await fetch(`/api/group/members?groupId=${encodeURIComponent(groupId)}`)
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
  const data = await resp.json()
  return {
    members: data.members ?? [],
    maxRounds: Number(data.maxRounds) || 10,
    mode: data.mode === 'free' ? 'free' : 'host',
    parallelDefault: data.parallelDefault === true,
  }
}

/** addGroupMembers adds one or more agents to the group. */
export async function addGroupMembers(groupId: string, agentIds: string[]): Promise<string[]> {
  const data = await postJSON('/api/group/members', { groupId, agentIds })
  return (data.memberIds as string[]) ?? []
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

/** updateGroupSettings updates the group's max rounds. */
export async function updateGroupSettings(groupId: string, maxRounds: number): Promise<void> {
  const resp = await fetch('/api/group/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, maxRounds }),
  })
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}

/** setGroupParallelDefault updates the free-mode "并发执行" switch: when on, the
 *  user's @-mentions run concurrently. Sends ONLY parallelDefault (the backend
 *  treats both settings as optional), so it cannot clobber maxRounds. */
export async function setGroupParallelDefault(groupId: string, enabled: boolean): Promise<void> {
  const resp = await fetch('/api/group/settings', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ groupId, parallelDefault: enabled }),
  })
  if (!resp.ok) throw new Error(`request failed: ${resp.status}`)
}

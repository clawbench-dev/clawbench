import { describe, expect, it } from 'vitest'
import { findMemberAgentCard } from '@/utils/teamMemberCard'

// The lookup exists to reach a member's FULL prompt: the roster identifies a
// member by name only, while the prompt lives on the Agent card's
// `chat_tool_calls` row. These tests pin both attribution sources and the
// newest-first rule, since getting either wrong opens the wrong member's card.

describe('findMemberAgentCard', () => {
  it('finds a card by its own member_name', () => {
    const messages = [{
      id: 11,
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_a', member_name: 'alpha' },
        { type: 'text', text: 'hi', parent_tool_call_id: 'call_a' },
      ],
    }]
    expect(findMemberAgentCard(messages, 'alpha')).toEqual({ toolId: 'call_a', msgId: 11 })
  })

  // Older rows: the card has no attribution, but its children do.
  it('derives the card from a child when the card itself has no member_name', () => {
    const messages = [{
      id: 12,
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_b', display_name: 'general-purpose' },
        { type: 'thinking', text: 'x', parent_tool_call_id: 'call_b', member_name: 'beta' },
      ],
    }]
    expect(findMemberAgentCard(messages, 'beta')).toEqual({ toolId: 'call_b', msgId: 12 })
  })

  it('prefers the newest card when a member was respawned', () => {
    const messages = [
      { id: 1, blocks: [{ type: 'tool_use', name: 'Agent', id: 'call_old', member_name: 'alpha' }] },
      { id: 2, blocks: [{ type: 'tool_use', name: 'Agent', id: 'call_new', member_name: 'alpha' }] },
    ]
    expect(findMemberAgentCard(messages, 'alpha')).toEqual({ toolId: 'call_new', msgId: 2 })
  })

  it('ignores non-Agent tools that happen to carry member_name', () => {
    // A member's own Bash/SendMessage cards carry member_name too; returning one
    // would open a tool detail instead of the member's Agent card.
    const messages = [{
      id: 3,
      blocks: [
        { type: 'tool_use', name: 'Bash', id: 'call_bash', member_name: 'alpha' },
        { type: 'tool_use', name: 'SendMessage', id: 'call_send', member_name: 'alpha' },
      ],
    }]
    expect(findMemberAgentCard(messages, 'alpha')).toBeNull()
  })

  it('matches the generic Task host as well as Agent', () => {
    const messages = [{
      id: 4,
      blocks: [{ type: 'tool_use', name: 'task', id: 'call_t', member_name: 'gamma' }],
    }]
    expect(findMemberAgentCard(messages, 'gamma')).toEqual({ toolId: 'call_t', msgId: 4 })
  })

  it('returns null for an unknown member, empty input, or a missing name', () => {
    const messages = [{ id: 5, blocks: [{ type: 'tool_use', name: 'Agent', id: 'a', member_name: 'alpha' }] }]
    expect(findMemberAgentCard(messages, 'nobody')).toBeNull()
    expect(findMemberAgentCard(null, 'alpha')).toBeNull()
    expect(findMemberAgentCard(messages, '')).toBeNull()
    expect(findMemberAgentCard([{ id: 6 }], 'alpha')).toBeNull()
    expect(findMemberAgentCard([{ id: 7, blocks: null }], 'alpha')).toBeNull()
  })

  it('does not match a card whose child belongs to a different member', () => {
    // Two members spawned in one message: each card must resolve to its own
    // tool id, or both rows would open the same prompt.
    const messages = [{
      id: 8,
      blocks: [
        { type: 'tool_use', name: 'Agent', id: 'call_1' },
        { type: 'tool_use', name: 'Agent', id: 'call_2' },
        { type: 'text', text: 'a', parent_tool_call_id: 'call_1', member_name: 'alpha' },
        { type: 'text', text: 'b', parent_tool_call_id: 'call_2', member_name: 'beta' },
      ],
    }]
    expect(findMemberAgentCard(messages, 'alpha')).toEqual({ toolId: 'call_1', msgId: 8 })
    expect(findMemberAgentCard(messages, 'beta')).toEqual({ toolId: 'call_2', msgId: 8 })
  })
})

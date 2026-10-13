import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  ensureSkillsLoaded,
  skillMenuRows,
  useSkillsState,
  type SkillRow,
} from '@/composables/useSkillsState'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The module-level singleton persists across tests in this file, so reset the
// pieces each test cares about before it runs.
const { skills, loaded } = useSkillsState()

function skill(over: Partial<SkillRow>): SkillRow {
  return {
    name: 'x',
    description: '',
    path: '/p/x/SKILL.md',
    source_kind: 'user',
    source_label: 'user dir',
    ...over,
  }
}

beforeEach(() => {
  skills.value = []
  loaded.value = false
  vi.restoreAllMocks()
})

describe('ensureSkillsLoaded', () => {
  it('fetches once and then reuses the loaded list', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ skills: [skill({ name: 'pdf' })] }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    await ensureSkillsLoaded()
    expect(skills.value.map((s) => s.name)).toEqual(['pdf'])
    expect(fetchMock).toHaveBeenCalledTimes(1)

    // A second call must not hit the network again.
    await ensureSkillsLoaded()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('marks loaded even on an empty list, so it does not refetch every keystroke', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ skills: [] }),
    }))
    vi.stubGlobal('fetch', fetchMock)

    await ensureSkillsLoaded()
    await ensureSkillsLoaded()
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})

describe('skillMenuRows', () => {
  it('drops skills whose frontmatter name disagrees with the directory', () => {
    skills.value = [
      skill({ name: 'good' }),
      skill({ name: 'bad', name_mismatch: true }),
    ]
    expect(skillMenuRows().map((s) => s.name)).toEqual(['good'])
  })

  it('dedupes by canonical name, keeping the highest-priority source', () => {
    skills.value = [
      skill({ name: 'dup', source_kind: 'other', description: 'other' }),
      skill({ name: 'dup', source_kind: 'user', description: 'user' }),
      skill({ name: 'dup', source_kind: 'project', description: 'project' }),
    ]
    const rows = skillMenuRows()
    expect(rows).toHaveLength(1)
    expect(rows[0].description).toBe('project')
  })

  it('treats a leading slash and case as the same identity', () => {
    skills.value = [
      skill({ name: '/MMX-CLI', source_kind: 'user' }),
      skill({ name: 'mmx-cli', source_kind: 'git' }),
    ]
    expect(skillMenuRows()).toHaveLength(1)
  })

  it('sorts by name', () => {
    skills.value = [skill({ name: 'zeta' }), skill({ name: 'alpha' })]
    expect(skillMenuRows().map((s) => s.name)).toEqual(['alpha', 'zeta'])
  })
})

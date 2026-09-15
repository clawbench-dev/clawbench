import { describe, expect, it } from 'vitest'
import { extractRemoteHost, isOfficialForgeHost, isNonOfficialRemote } from '@/utils/forgeHost'

/**
 * These helpers decide whether the bind dialog warns before sending a
 * credential to a host the server does not vouch for.
 *
 * The cases below pin the two ways that can silently go wrong: failing to
 * recognise a non-official host (no warning where one is owed), and flagging an
 * official one (a warning on the common path, which trains users to ignore it).
 */
describe('extractRemoteHost', () => {
  it('reads the host from https and ssh URLs', () => {
    expect(extractRemoteHost('https://gitlab.com/acme/widgets.git')).toBe('gitlab.com')
    expect(extractRemoteHost('https://git.internal.corp:8443/acme/widgets.git')).toBe('git.internal.corp:8443')
    expect(extractRemoteHost('ssh://git@github.com/acme/widgets.git')).toBe('github.com')
    expect(extractRemoteHost('ssh://git@10.0.0.5:2222/acme/widgets.git')).toBe('10.0.0.5:2222')
  })

  it('reads the host from the scp-like form', () => {
    // This is the form `git remote -v` shows for most SSH clones, so missing it
    // would silently skip the warning for a very common case.
    expect(extractRemoteHost('git@github.com:acme/widgets.git')).toBe('github.com')
    expect(extractRemoteHost('git@git.internal.corp:group/sub/widgets.git')).toBe('git.internal.corp')
    expect(extractRemoteHost('git@192.168.1.50:acme/widgets.git')).toBe('192.168.1.50')
  })

  it('preserves the host as written for the scp form', () => {
    // URL parsing lowercases the hostname (per spec), but the scp branch does
    // not. Callers must therefore not compare hosts for equality without
    // normalizing — isOfficialForgeHost lowercases for exactly this reason.
    expect(extractRemoteHost('https://GitHub.com/acme/widgets.git')).toBe('github.com')
    expect(extractRemoteHost('git@GitHub.com:acme/widgets.git')).toBe('GitHub.com')
  })

  it('returns empty for forms the backend does not accept', () => {
    // A local path, a bare host, a file:// URL and an unsupported scheme are all
    // rejected by ParseRemoteURL, so the dialog shows that error instead.
    expect(extractRemoteHost('/srv/git/widgets.git')).toBe('')
    expect(extractRemoteHost('file:///srv/git/widgets.git')).toBe('')
    expect(extractRemoteHost('http://gitlab.com/acme/widgets.git')).toBe('')
    expect(extractRemoteHost('github.com')).toBe('')
    expect(extractRemoteHost('')).toBe('')
    expect(extractRemoteHost('   ')).toBe('')
  })

  it('returns empty for malformed scp-like input', () => {
    expect(extractRemoteHost('git@github.com')).toBe('')       // no colon
    expect(extractRemoteHost('git@:acme/widgets.git')).toBe('') // empty host
    expect(extractRemoteHost('git@github.com:')).toBe('')       // empty path
  })
})

describe('isOfficialForgeHost', () => {
  it('accepts exactly the platform-operated hosts', () => {
    expect(isOfficialForgeHost('github.com')).toBe(true)
    expect(isOfficialForgeHost('gitlab.com')).toBe(true)
    // Case-insensitive, since git remotes are written both ways.
    expect(isOfficialForgeHost('GitHub.com')).toBe(true)
    expect(isOfficialForgeHost('  gitlab.com  ')).toBe(true)
  })

  it('rejects a port, mirroring the backend auto-bind gate', () => {
    // isOfficialForgeHost in the handler deliberately does not strip the port:
    // auto-binding github.com:8443 would send the token to
    // https://github.com:8443/api/v3. The warning must agree with that.
    expect(isOfficialForgeHost('github.com:443')).toBe(false)
    expect(isOfficialForgeHost('github.com:8443')).toBe(false)
  })

  it('rejects self-hosted and private hosts', () => {
    expect(isOfficialForgeHost('gitlab.example.com')).toBe(false)
    expect(isOfficialForgeHost('127.0.0.1')).toBe(false)
    expect(isOfficialForgeHost('10.0.0.5:8443')).toBe(false)
    expect(isOfficialForgeHost('')).toBe(false)
  })
})

describe('isNonOfficialRemote', () => {
  it('flags a self-hosted remote in every accepted form', () => {
    // The exact scenario this feature exists for: an internal GitLab.
    expect(isNonOfficialRemote('https://git.internal.corp:8443/acme/widgets.git')).toBe(true)
    expect(isNonOfficialRemote('git@git.internal.corp:acme/widgets.git')).toBe(true)
    expect(isNonOfficialRemote('https://192.168.1.50/acme/widgets.git')).toBe(true)
  })

  it('does not flag the official hosts', () => {
    expect(isNonOfficialRemote('https://github.com/acme/widgets.git')).toBe(false)
    expect(isNonOfficialRemote('git@gitlab.com:acme/widgets.git')).toBe(false)
  })

  it('does not flag input it cannot parse', () => {
    // No warning for a local path: the backend rejects the URL outright, and
    // "non-official host" would be the wrong explanation for that failure.
    expect(isNonOfficialRemote('/srv/git/widgets.git')).toBe(false)
    expect(isNonOfficialRemote('')).toBe(false)
  })
})

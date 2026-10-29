import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

/**
 * Share link construction across every address the server is reachable on.
 *
 * The behaviour that matters is the CHOICE of primary link. A machine with
 * Wi-Fi + Ethernet + Docker has three addresses and only some of them work for
 * a colleague; picking the wrong one as "the" link is the whole bug this
 * feature exists to fix.
 */

let fetchMock: ReturnType<typeof vi.fn>

function stubAddresses(body: unknown, ok = true) {
  fetchMock.mockResolvedValue({
    ok,
    status: ok ? 200 : 500,
    json: async () => body,
  })
}

/** Point window.location at a given origin for one test. */
function setLocation(href: string) {
  const u = new URL(href)
  vi.stubGlobal('location', {
    origin: u.origin,
    protocol: u.protocol,
    hostname: u.hostname,
    port: u.port,
    host: u.host,
  })
}

beforeEach(async () => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
  setLocation('http://192.168.100.104:20000/')
  const mod = await import('../useShareLinks')
  mod._resetShareAddressesForTest()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

async function build(path = '/share/tok') {
  const { buildShareLinks } = await import('../useShareLinks')
  return buildShareLinks(path)
}

describe('buildShareLinks', () => {
  it('promotes the address the browser is already using', async () => {
    // The sharer reached the server on .142, so that is the best evidence of
    // an address that works on this network — even though .104 sorts first.
    setLocation('http://192.168.100.142:20000/')
    stubAddresses({
      port: '20000',
      addresses: [
        { ip: '192.168.100.104', interface: 'wlp1s0', kind: 'lan' },
        { ip: '192.168.100.142', interface: 'enx0826ae3967ba', kind: 'lan' },
        { ip: '172.17.0.1', interface: 'docker0', kind: 'virtual' },
      ],
    })

    const links = await build()

    expect(links[0].url).toBe('http://192.168.100.142:20000/share/tok')
    expect(links).toHaveLength(3)
  })

  it('keeps virtual adapters in the list, labelled', async () => {
    stubAddresses({
      port: '20000',
      addresses: [
        { ip: '192.168.100.104', interface: 'wlp1s0', kind: 'lan' },
        { ip: '172.17.0.1', interface: 'docker0', kind: 'virtual' },
      ],
    })

    const links = await build()

    // Hiding the Docker bridge would leave the user unable to explain why an
    // address does not work; it must be present, just ranked last.
    expect(links.map((l) => l.address.ip)).toContain('172.17.0.1')
    expect(links[links.length - 1].address.kind).toBe('virtual')
  })

  it('sorts LAN before virtual and IPv4 before IPv6', async () => {
    // Point the browser at one of the listed addresses so it is promoted, then
    // the REST must still be ordered LAN(v4) → LAN(v6) → virtual.
    setLocation('http://192.168.1.5:20000/')
    stubAddresses({
      port: '20000',
      addresses: [
        { ip: '2001:db8::1', interface: 'eth0', kind: 'lan' },
        { ip: '172.17.0.1', interface: 'docker0', kind: 'virtual' },
        { ip: '192.168.1.5', interface: 'eth0', kind: 'lan' },
      ],
    })

    const links = await build()

    expect(links.map((l) => l.address.ip)).toEqual([
      '192.168.1.5', // primary (current)
      '2001:db8::1', // remaining LAN, IPv6 after no IPv4 is left
      '172.17.0.1', // virtual last
    ])
  })

  it('keeps the browser origin as primary when it is not a listed address', async () => {
    // localhost / a hostname / a reverse proxy: the browser's own origin is
    // still the one we know works, so it must stay first.
    setLocation('http://localhost:20000/')
    stubAddresses({
      port: '20000',
      addresses: [{ ip: '192.168.100.104', interface: 'wlp1s0', kind: 'lan' }],
    })

    const links = await build()

    expect(links[0].url).toBe('http://localhost:20000/share/tok')
    expect(links[0].address.kind).toBe('current')
    expect(links[1].url).toBe('http://192.168.100.104:20000/share/tok')
  })

  it('degrades to a single origin-based link when the endpoint fails', async () => {
    stubAddresses({}, false)

    const links = await build()

    // A failed address lookup must not break sharing: the old behaviour is
    // still correct, just less useful.
    expect(links).toHaveLength(1)
    expect(links[0].url).toBe('http://192.168.100.104:20000/share/tok')
  })

  it('degrades when the server reports no addresses', async () => {
    stubAddresses({ port: '20000', addresses: [] })

    const links = await build()

    expect(links).toHaveLength(1)
    expect(links[0].url).toContain('/share/tok')
  })

  it('uses the browser port, not the server-reported one', async () => {
    // Behind a port-forward the public port differs from the listening port;
    // the browser's own port is the one that actually works.
    setLocation('http://192.168.1.5:8080/')
    stubAddresses({
      port: '20000',
      addresses: [{ ip: '192.168.1.5', interface: 'eth0', kind: 'lan' }],
    })

    const links = await build()

    expect(links[0].url).toBe('http://192.168.1.5:8080/share/tok')
  })

  it('omits the port when it is the scheme default', async () => {
    setLocation('https://example.com/')
    stubAddresses({
      port: '443',
      addresses: [{ ip: '203.0.113.7', interface: 'eth0', kind: 'public' }],
    })

    const links = await build()

    expect(links[1].url).toBe('https://203.0.113.7/share/tok')
  })

  it('brackets IPv6 addresses', async () => {
    // The browser sits on an IPv6 address that IS listed, so it becomes the
    // primary and its URL must be bracketed.
    setLocation('http://[2001:db8::1]:20000/')
    stubAddresses({
      port: '20000',
      addresses: [{ ip: '2001:db8::1', interface: 'eth0', kind: 'lan' }],
    })

    const links = await build()

    // A bare IPv6 in a URL is invalid; it must be bracketed.
    expect(links[0].url).toBe('http://[2001:db8::1]:20000/share/tok')
  })

  it('fetches the address list only once per page load', async () => {
    stubAddresses({
      port: '20000',
      addresses: [{ ip: '192.168.100.104', interface: 'wlp1s0', kind: 'lan' }],
    })

    await build('/share/a')
    await build('/share/b')

    // Two dialogs opened in one session must not each pay a round trip.
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('does not cache a failure', async () => {
    stubAddresses({}, false)
    await build()
    stubAddresses({
      port: '20000',
      addresses: [{ ip: '192.168.100.104', interface: 'wlp1s0', kind: 'lan' }],
    })

    const links = await build()

    // A transient error must not pin the degraded result for the whole session.
    expect(links).toHaveLength(1)
    expect(links[0].address.ip).toBe('192.168.100.104')
  })
})

describe('useShareLinks', () => {
  it('exposes the primary link and the full list', async () => {
    setLocation('http://192.168.100.142:20000/')
    stubAddresses({
      port: '20000',
      addresses: [
        { ip: '192.168.100.104', interface: 'wlp1s0', kind: 'lan' },
        { ip: '192.168.100.142', interface: 'enx', kind: 'lan' },
      ],
    })
    const { useShareLinks } = await import('../useShareLinks')
    const s = useShareLinks()

    await s.load('/share/tok')

    expect(s.primary()).toBe('http://192.168.100.142:20000/share/tok')
    expect(s.all.value).toHaveLength(2)
  })
})

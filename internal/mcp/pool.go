package mcp

// The pool exists because a chat turn is not a CLI invocation: spawning
// `npx @modelcontextprotocol/server-github` on every message costs seconds per
// server and throws away whatever state the server accumulated. ACP solves the
// same problem with one cached connection per agent; this is the same shape for
// in-process runs.
//
// Caching brings three obligations, and they are the whole design:
//
//   - a turn must not close a connection another turn is using (refcount),
//   - a config or policy change must not keep running the old server
//     (fingerprint), and
//   - an unused server must not live forever (idle reap).
//
// A server that fails to connect is NOT an error for the turn: it becomes a
// note the model can relay. One broken entry in a list of five must not make
// the agent unusable.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"clawbench/internal/model"
)

const (
	// defaultIdleTimeout is how long a connection with no turns is kept. Long
	// enough that a conversation with pauses does not pay the startup cost
	// again, short enough that a forgotten server does not hold memory forever.
	defaultIdleTimeout = 10 * time.Minute
	// reapInterval is how often idle connections are checked.
	reapInterval = time.Minute
	// maxConcurrentDials bounds parallel server starts. Each dial may spawn an
	// interpreter; starting twenty at once on a laptop is how a turn times out
	// for reasons unrelated to any one server.
	maxConcurrentDials = 4
	// defaultListRefreshInterval throttles tools/list for servers that
	// advertise listChanged. Re-listing on every turn would put a round trip in
	// front of every model call; never re-listing would make a newly added tool
	// invisible until reconnect. 30s is the compromise.
	defaultListRefreshInterval = 30 * time.Second
)

// ConnectedServer is one server with its current tool list.
//
// Tools can change while the connection is open (a server that advertises
// listChanged), so access goes through the mutex rather than exposing the
// slice.
type ConnectedServer struct {
	Name    string
	Session *Session
	// spec is what was dialed. Kept so a dead connection can be replaced
	// without asking the caller for the server definition again.
	spec model.MCPServer

	mu          sync.RWMutex
	tools       []ToolDef
	lastRefresh time.Time
}

// ServerName is the configured name of this server.
func (c *ConnectedServer) ServerName() string { return c.Name }

// Tools returns a copy of the current tool list.
func (c *ConnectedServer) Tools() []ToolDef {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]ToolDef(nil), c.tools...)
}

func (c *ConnectedServer) setTools(tools []ToolDef) {
	c.mu.Lock()
	c.tools = tools
	c.mu.Unlock()
}

// claimRefresh reports whether this server is due for a tool-list refresh and,
// if so, marks it as done. One check-and-set under the lock keeps two
// concurrent turns from both paying for the same round trip.
func (c *ConnectedServer) claimRefresh(now time.Time, interval time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.lastRefresh) < interval {
		return false
	}
	c.lastRefresh = now
	return true
}

// Call invokes one tool on this server.
func (c *ConnectedServer) Call(ctx context.Context, tool string, args json.RawMessage) (CallResult, error) {
	return c.Session.CallTool(ctx, tool, args)
}

// Bundle is one turn's view of the pool. Release must be called exactly once.
type Bundle struct {
	Servers []*ConnectedServer
	// Notes explains servers that could not be attached, in user-readable
	// terms. They are surfaced to the model so "the tool is missing" is
	// explained instead of silently true.
	Notes   []string
	release func()
}

// Release returns this turn's reference.
func (b *Bundle) Release() {
	if b != nil && b.release != nil {
		b.release()
	}
}

// Pool caches connections per key.
type Pool struct {
	idleTimeout         time.Duration
	listRefreshInterval time.Duration

	mu      sync.Mutex
	entries map[string]*poolEntry
	started bool
	stop    chan struct{}
	// closed is atomic so Acquire can check it while holding an entry lock
	// without taking mu (CloseAll takes mu and then entry locks; the reverse
	// order would deadlock).
	closed atomic.Bool
}

type poolEntry struct {
	mu          sync.Mutex
	fingerprint string
	// dialed records that this fingerprint was already attempted. It is
	// deliberately separate from "servers is non-empty": an entry where every
	// server failed must ALSO be cached, otherwise a permanently broken config
	// would re-run the 20s connect timeout on every single turn.
	dialed  bool
	servers []*ConnectedServer
	// retired holds servers replaced while a turn still held them. Closing them
	// at that moment would break the in-flight turn's tool call, so they are
	// closed when the last reference goes away.
	retired  []*ConnectedServer
	notes    []string
	refs     int
	lastUsed time.Time
}

var defaultPool = NewPool(defaultIdleTimeout)

// DefaultPool is the process-wide pool used by the built-in agent.
func DefaultPool() *Pool { return defaultPool }

// CloseAllSessions closes every cached connection. Called on server shutdown so
// stdio children do not outlive the process that started them.
func CloseAllSessions() { defaultPool.CloseAll() }

// NewPool creates a pool. A non-positive idleTimeout disables reaping, which is
// what tests that drive reap() by hand want.
func NewPool(idleTimeout time.Duration) *Pool {
	return &Pool{
		idleTimeout:         idleTimeout,
		listRefreshInterval: defaultListRefreshInterval,
		entries:             make(map[string]*poolEntry),
		stop:                make(chan struct{}),
	}
}

// Acquire returns the servers for one turn, dialing them if needed.
//
// It never returns an error: failures are reported in Bundle.Notes so the turn
// can proceed with whatever did connect.
func (p *Pool) Acquire(ctx context.Context, key string, servers []model.MCPServer, opts DialOptions) *Bundle {
	if len(servers) == 0 {
		return &Bundle{}
	}
	fingerprint := serversFingerprint(servers)

	e := p.entry(key)
	e.mu.Lock()

	if p.closed.Load() {
		e.mu.Unlock()
		return &Bundle{Notes: []string{"MCP connections are shutting down"}}
	}

	now := time.Now()
	if e.dialed && e.fingerprint == fingerprint {
		// A connection that was healthy last turn may have died since (a stdio
		// process crashed, an HTTP session expired). Reusing it would make every
		// future turn fail with "connection closed" until the config changes, so
		// replace it now. They are already unusable, which is why closing them
		// cannot interrupt a turn that still holds one.
		alive, dead := splitAlive(e.servers)
		if len(dead) > 0 {
			// Retire rather than close: another turn may still hold this bundle,
			// and an HTTP session that failed one request can have a call in
			// flight. release closes them when the last reference goes away.
			e.servers = alive
			e.retired = append(e.retired, dead...)
			specs := make([]model.MCPServer, 0, len(dead))
			for _, cs := range dead {
				specs = append(specs, cs.spec)
			}
			dialed, dialNotes := dialAll(ctx, specs, opts)
			e.servers = append(e.servers, dialed...)
			e.notes = append(e.notes, dialNotes...)
		}

		e.refs++
		e.lastUsed = now
		cached := e.servers
		// Copy the notes while the lock is held: a concurrent redial rewrites
		// e.notes, and reading the slice outside the lock would race it.
		notes := append([]string(nil), e.notes...)
		e.mu.Unlock()

		// A server that advertises listChanged may have gained or lost tools
		// since the last turn. Refresh those outside the lock: this is a round
		// trip, and other turns for the same agent must not wait behind it.
		refreshChangedTools(ctx, cached, p.listRefreshInterval)
		return &Bundle{Servers: cached, Notes: notes, release: func() { p.release(e) }}
	}

	// Stale or absent: retire the old connections while holding this key's lock
	// so two concurrent turns cannot both dial the same servers. They are
	// closed on the last Release, not now: a turn may still be using them.
	e.retired = append(e.retired, e.servers...)
	e.servers = nil
	e.notes = nil

	dialed, notes := dialAll(ctx, servers, opts)
	e.fingerprint = fingerprint
	e.dialed = true
	e.servers = dialed
	e.notes = notes
	e.refs++
	e.lastUsed = time.Now()
	e.mu.Unlock()

	p.startReaper()
	return &Bundle{Servers: dialed, Notes: append([]string(nil), notes...), release: func() { p.release(e) }}
}

func (p *Pool) release(e *poolEntry) {
	e.mu.Lock()
	if e.refs > 0 {
		e.refs--
	}
	e.lastUsed = time.Now()
	var retired []*ConnectedServer
	if e.refs == 0 && len(e.retired) > 0 {
		retired = e.retired
		e.retired = nil
	}
	e.mu.Unlock()

	closeServers(retired)
}

func (p *Pool) entry(key string) *poolEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.entries[key]
	if !ok {
		e = &poolEntry{}
		p.entries[key] = e
	}
	return e
}

func (p *Pool) startReaper() {
	p.mu.Lock()
	if p.started || p.idleTimeout <= 0 || p.closed.Load() {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.mu.Unlock()

	go func() {
		ticker := time.NewTicker(reapInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				p.reap(now)
			case <-p.stop:
				return
			}
		}
	}()
}

// reap closes entries that no turn holds and that have been idle too long.
//
// The entry itself stays in the map: deleting it would race with a turn that
// looked it up just before the reap, and a duplicate entry means a duplicate
// server process. An empty entry costs a few dozen bytes and is reused the next
// time the same agent runs.
func (p *Pool) reap(now time.Time) {
	p.mu.Lock()
	snapshot := make(map[string]*poolEntry, len(p.entries))
	for k, e := range p.entries {
		snapshot[k] = e
	}
	p.mu.Unlock()

	for _, e := range snapshot {
		e.mu.Lock()
		idle := e.refs == 0 && e.dialed && now.Sub(e.lastUsed) > p.idleTimeout
		var old []*ConnectedServer
		if idle {
			old = append(old, e.servers...)
			old = append(old, e.retired...)
			e.servers = nil
			e.retired = nil
			e.notes = nil
			e.dialed = false
			e.fingerprint = ""
		}
		e.mu.Unlock()

		if idle {
			closeServers(old)
		}
	}
}

// CloseAll closes every connection and refuses new ones.
func (p *Pool) CloseAll() {
	if p.closed.Swap(true) {
		return
	}
	p.mu.Lock()
	close(p.stop)
	entries := p.entries
	p.entries = make(map[string]*poolEntry)
	p.mu.Unlock()

	for _, e := range entries {
		e.mu.Lock()
		servers := append(append([]*ConnectedServer(nil), e.servers...), e.retired...)
		e.servers = nil
		e.retired = nil
		e.mu.Unlock()
		closeServers(servers)
	}
}

// dialAll connects to every server in parallel and reports the failures.
func dialAll(ctx context.Context, servers []model.MCPServer, opts DialOptions) ([]*ConnectedServer, []string) {
	type result struct {
		server *ConnectedServer
		note   string
	}
	results := make([]result, len(servers))

	sem := make(chan struct{}, maxConcurrentDials)
	var wg sync.WaitGroup
	for i, srv := range servers {
		wg.Add(1)
		go func(i int, srv model.MCPServer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			session, err := Dial(ctx, srv, opts)
			if err != nil {
				results[i] = result{note: fmt.Sprintf("%s: %v", srv.Name, err)}
				return
			}
			tools, err := session.ListTools(ctx)
			if err != nil {
				_ = session.Close()
				results[i] = result{note: fmt.Sprintf("%s: %v", srv.Name, err)}
				return
			}
			results[i] = result{server: &ConnectedServer{
				Name:        srv.Name,
				Session:     session,
				spec:        srv,
				tools:       tools,
				lastRefresh: time.Now(),
			}}
		}(i, srv)
	}
	wg.Wait()

	out := make([]*ConnectedServer, 0, len(servers))
	var notes []string
	for _, r := range results {
		if r.server != nil {
			out = append(out, r.server)
		}
		if r.note != "" {
			notes = append(notes, r.note)
		}
	}
	return out, notes
}

// refreshChangedTools re-lists tools only for servers that said the list can
// change. A server that never advertised listChanged keeps its cached list,
// which is the common case and costs nothing.
func refreshChangedTools(ctx context.Context, servers []*ConnectedServer, interval time.Duration) {
	now := time.Now()
	var wg sync.WaitGroup
	for _, cs := range servers {
		if !cs.Session.ListChanged() || !cs.claimRefresh(now, interval) {
			continue
		}
		wg.Add(1)
		go func(cs *ConnectedServer) {
			defer wg.Done()
			listCtx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
			defer cancel()
			if tools, err := cs.Session.ListTools(listCtx); err == nil {
				cs.setTools(tools)
			}
		}(cs)
	}
	wg.Wait()
}

// splitAlive partitions connections by whether they can still serve calls.
func splitAlive(servers []*ConnectedServer) (alive, dead []*ConnectedServer) {
	alive = make([]*ConnectedServer, 0, len(servers))
	for _, cs := range servers {
		if cs.Session.Alive() {
			alive = append(alive, cs)
			continue
		}
		dead = append(dead, cs)
	}
	return alive, dead
}

func closeServers(servers []*ConnectedServer) {
	for _, cs := range servers {
		if cs != nil && cs.Session != nil {
			_ = cs.Session.Close()
		}
	}
}

// serversFingerprint identifies "the same set of servers with the same
// settings". encoding/json sorts map keys, so env/header maps cannot make the
// hash unstable — an unstable hash would reconnect on every turn.
func serversFingerprint(servers []model.MCPServer) string {
	b, err := json.Marshal(servers)
	if err != nil {
		// Unreachable for these types; fall back to names so a hash failure
		// degrades to "reuse anything with the same names".
		names := make([]string, 0, len(servers))
		for _, s := range servers {
			names = append(names, s.Name)
		}
		b, _ = json.Marshal(names)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

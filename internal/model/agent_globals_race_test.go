package model

import (
	"sync"
	"testing"
)

// TestAgentGlobalsConcurrentAccessIsRaceFree 断言访问器与整体替换之间没有数据竞争。
// 运行方式（必须带 -race 才有意义）：
//
//	go test ./internal/model/ -run TestAgentGlobalsConcurrentAccessIsRaceFree -race
//
// 背景：异步模型发现（StartModelDiscoveryAsync）会在服务已接受请求之后，
// 于后台 goroutine 里整体替换 Agents/AgentList。若读者直接索引全局 map，
// Go runtime 会报 "concurrent map read and map write"（未定义行为）。
// 本测试并发地跑「读者走访问器」与「写者走 ReplaceAgents」，-race 下必须干净。
func TestAgentGlobalsConcurrentAccessIsRaceFree(t *testing.T) {
	origList := GetAgentList()
	restore := func() { ReplaceAgents(map[string]*Agent{}, nil) }
	_ = origList
	defer restore()

	build := func(n int) (map[string]*Agent, []*Agent) {
		m := make(map[string]*Agent, n)
		l := make([]*Agent, 0, n)
		for i := range n {
			a := &Agent{ID: string(rune('a' + i)), Backend: "backend"}
			m[a.ID] = a
			l = append(l, a)
		}
		return m, l
	}

	const iterations = 200
	var wg sync.WaitGroup

	// Writer: repeatedly replace the whole set (what the async reload does).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			m, l := build(3)
			ReplaceAgents(m, l)
		}
	}()

	// Readers: every accessor that production uses.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				_ = GetAgent("a")
				_ = GetAgentList()
				_ = HasAgent("a")
				_ = AgentSetLoaded()
				_ = GetDefaultAgentID()
			}
		}()
	}

	wg.Wait()
}

// TestAgentGlobalsConcurrentFieldEditIsRaceFree pins the other half of the
// contract: the in-place field editor (UpdateAgent) must be safe against both
// the accessor readers and a whole-set ReplaceAgents. The handler's own
// configMutex does not cover readers in internal/service / internal/ai, so
// agentsMu is what actually serializes these field writes.
//
//	go test ./internal/model/ -run TestAgentGlobalsConcurrentFieldEditIsRaceFree -race
func TestAgentGlobalsConcurrentFieldEditIsRaceFree(t *testing.T) {
	restore := func() { ReplaceAgents(map[string]*Agent{}, nil) }
	defer restore()

	m := map[string]*Agent{"a": {ID: "a"}}
	ReplaceAgents(m, []*Agent{m["a"]})

	const iterations = 200
	var wg sync.WaitGroup

	// Writer: in-place field edits (what the agent PATCH handler does).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			UpdateAgent("a", func(a *Agent) {
				a.Name = "n"
				a.PreferredModel = "m"
			})
		}
	}()

	// Writer: whole-set replacement (what the async model discovery does).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range iterations {
			a := &Agent{ID: "a"}
			ReplaceAgents(map[string]*Agent{"a": a}, []*Agent{a})
		}
	}()

	// Readers: read the fields off the pointer the accessors hand out.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				if a := GetAgent("a"); a != nil {
					_ = a.Name
					_ = a.PreferredModel
				}
				for _, a := range GetAgentList() {
					_ = a.Name
				}
			}
		}()
	}

	wg.Wait()
}

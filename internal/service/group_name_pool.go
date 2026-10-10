package service

// groupNamePool is the built-in pool of English display names used to
// disambiguate DUPLICATE instances of one agent inside a group. The FIRST
// instance keeps the agent's own name; the 2nd, 3rd, … draw the next unused
// pool name. When the pool is exhausted, naming falls back to
// "<agent name> <ordinal>" (the pre-pool behavior).
//
// It is deliberately NOT configurable (no config field, no settings UI): the
// names are intentionally language-neutral English tokens so they never collide
// with the group routing protocol's reserved characters and read the same in
// every UI locale. Drawn names are unique within a group and never reused.
var groupNamePool = []string{
	"Alice", "Bob", "Carol", "Dave", "Erin", "Frank", "Grace", "Heidi",
	"Ivan", "Judy", "Karl", "Laura", "Mallory", "Nina", "Olivia", "Peggy",
	"Quinn", "Rupert", "Sybil", "Trent", "Uma", "Victor", "Wendy", "Xena",
	"Yara", "Zane", "Aaron", "Bella", "Cody", "Diana", "Ethan", "Fiona",
	"Gavin", "Hazel", "Ian", "Julia", "Kevin", "Luna", "Milo", "Nora",
}

// nextPoolName returns the first pool name not present in used, or "" when the
// whole pool is taken.
func nextPoolName(used map[string]bool) string {
	for _, n := range groupNamePool {
		if !used[n] {
			return n
		}
	}
	return ""
}

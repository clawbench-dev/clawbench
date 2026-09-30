package handler

import (
	"net/http"
	"sort"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/skill"
)

// skills.go exposes the cross-agent skill discovery state and a manual sync
// trigger. The settings live in /api/config (skills.*); these endpoints report
// what was actually discovered and let the user force a git pull.

// jsonURL / jsonLastError are response keys spelled the same way here and in
// the sync-state writer below, so they are named once.
const (
	jsonURL       = "url"
	jsonLastError = "last_error"
)

// skillsListResponse is the payload of GET /api/skills.
type skillsListResponse struct {
	Enabled      bool              `json:"enabled"`
	Dirs         []string          `json:"dirs"`
	RefreshHours int               `json:"refresh_hours"`
	LastError    string            `json:"last_error"`
	LastSyncAt   int64             `json:"last_sync_at"`
	Repos        []configSkillRepo `json:"repos"`
	Skills       []skillInfoJSON   `json:"skills"`
}

// skillInfoJSON is one discovered skill. Source describes where it came from;
// AgentID lists the agent(s) whose spec declares the directory (empty for the
// shared .agents/skills when no installed agent reads it).
type skillInfoJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	SourceKind  string `json:"source_kind"`
	SourceLabel string `json:"source_label"`
	AgentID     string `json:"agent_id,omitempty"`
	// NameMismatch is true when the frontmatter name disagrees with the skill's
	// directory name. The Agent Skills spec requires them to match, and an agent
	// resolves a skill by directory — so such a skill is injected (the table
	// carries an explicit path) but is NOT offered as a slash command. The UI
	// flags it so the user can fix the skill.
	NameMismatch bool `json:"name_mismatch,omitempty"`
}

// ServeSkills handles GET /api/skills.
func ServeSkills(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	found := skill.Global().All()
	skills := make([]skillInfoJSON, 0, len(found))
	for _, s := range found {
		skills = append(skills, skillInfoJSON{
			Name:         s.Name,
			Description:  s.Description,
			Path:         s.Path,
			SourceKind:   s.Source.Kind.String(),
			SourceLabel:  s.Source.Label,
			AgentID:      s.Source.AgentID,
			NameMismatch: s.NameMismatch,
		})
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Path < skills[j].Path
	})

	writeJSON(w, http.StatusOK, skillsListResponse{
		Enabled:      cfg.Skills.Enabled,
		Dirs:         cfg.ResolveSkillsDirs(),
		RefreshHours: cfg.Skills.RefreshHours,
		LastError:    cfg.Skills.LastError,
		LastSyncAt:   cfg.Skills.LastSyncAt,
		Repos:        buildConfigSkills(cfg).Repos,
		Skills:       skills,
	})
}

// skillsRefreshResponse is the payload of POST /api/skills/refresh.
type skillsRefreshResponse struct {
	OK         bool              `json:"ok"`
	SkillCount int               `json:"skill_count"`
	Errors     map[string]string `json:"errors"`
}

// ServeSkillsRefresh handles POST /api/skills/refresh.
//
// A failure on one repository is reported per-repo in `errors` and does NOT
// make the request fail: the other repositories still synced, and a single
// unreachable remote is a normal condition the UI should surface inline rather
// than as a generic server error.
func ServeSkillsRefresh(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Bound the wait so a slow remote cannot hold the request open forever.
	// TriggerGitSync joins an in-flight pass rather than starting a second one.
	type outcome struct{ res skill.SyncResult }
	ch := make(chan outcome, 1)
	go func() {
		ch <- outcome{res: skill.TriggerGitSync()}
	}()

	select {
	case out := <-ch:
		errors := out.res.Errors
		if errors == nil {
			errors = map[string]string{}
		}
		writeJSON(w, http.StatusOK, skillsRefreshResponse{
			OK:         len(errors) == 0,
			SkillCount: out.res.SkillCount,
			Errors:     errors,
		})
	case <-time.After(3 * time.Minute):
		writeJSON(w, http.StatusAccepted, skillsRefreshResponse{
			OK:         true,
			SkillCount: len(skill.Global().All()),
			Errors:     map[string]string{},
		})
	}
}

// PersistSkillSyncState records the git sync worker's outcome in config. It is
// injected into the worker via skill.SetPersistSyncStateFn so the skill package
// never has to import this one.
//
// On a disk-write failure the in-memory config is restored, so the worker's
// view of the sync state never diverges from disk.
func PersistSkillSyncState(res skill.SyncResult) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	snapshot := model.ConfigInstance

	repos := make([]any, 0, len(model.ConfigInstance.Skills.Repos))
	for _, r := range model.ConfigInstance.Skills.Repos {
		repos = append(repos, map[string]any{
			jsonURL:       r.URL,
			jsonSlug:      r.Slug,
			"token":       r.Token,
			jsonLastError: r.LastError,
		})
	}

	patch := map[string]any{
		"skills": map[string]any{
			jsonLastError:  model.ConfigInstance.Skills.LastError,
			"last_sync_at": model.ConfigInstance.Skills.LastSyncAt,
			"repos":        repos,
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		return err
	}
	return nil
}

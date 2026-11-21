package service

import (
	"log/slog"
	"strconv"

	"clawbench/internal/store"
)

// This file is the notification center's ONE per-kind extension point.
//
// A kind declares two things here: how to read its subject id off a row, and
// where that subject lives. Everything downstream — the staleness probe, the
// `sourceMissing` flag and the pre-probe repair pass — iterates this table, so
// adding a kind cannot leave one of those passes behind. The alternative (the
// shape this replaced) was three parallel switches over the kind plus a
// hand-written collector per id-space, which silently mislabelled a task whose
// session had been purged.
//
// Adding a notification kind therefore means:
//
//  1. a NotificationKind* constant and one entry in notificationSubjects here;
//  2. a writer that calls StoreNotification (see notifications.go);
//  3. the frontend's kind table (web/src/utils/notificationKinds.ts).
//
// A kind with no local source (forge items live upstream; a usage threshold is
// an aggregate) is registered with no subject: it is declared, but never
// probed. Registering it anyway is what lets the completeness test insist that
// every kind is accounted for.

// notificationSubject says how one kind names and locates its source row.
//
// The zero value means "no local source": the row is never marked stale. That
// is a declaration, not an omission — see the forge/usage entries below.
type notificationSubject struct {
	// kind is the NotificationKind* value this entry describes.
	kind string

	// id / intID read the subject id off a row. Exactly one is set, selected
	// by integer: string ids are the norm (session/task/meeting), the mail
	// cache is the one integer id-space.
	id      func(*Notification) string
	intID   func(*Notification) int64
	integer bool

	// table / key locate the subject row. Both are allowlisted literals from
	// this file, never user input.
	table string
	key   string

	// repair optionally rewrites stored ids before the presence probe runs
	// (best effort, whole page at once). Only sessions need it today: legacy
	// rows written with a group MEMBER id must be pointed at the group
	// timeline, or the probe judges the wrong id.
	repair func([]Notification)
}

// hasSource reports whether the kind points at a local row that can disappear.
func (s notificationSubject) hasSource() bool {
	return s.id != nil || s.intID != nil
}

// idOf reads the subject id and reports whether the row carries one. An empty
// id means "this row has no subject", which is never stale.
func (s notificationSubject) idOf(n *Notification) (string, bool) {
	if s.integer {
		if id := s.intID(n); id > 0 {
			return strconv.FormatInt(id, 10), true
		}
		return "", false
	}
	if id := s.id(n); id != "" {
		return id, true
	}
	return "", false
}

// tableScheduledTasks is the scheduled-tasks table name. A constant because the
// name is also referenced by the migration/delete sweeps, and the linter flags
// the fourth literal occurrence.
const tableScheduledTasks = "scheduled_tasks"

// notificationSubjects is the registry. Order is irrelevant to behavior (every
// pass visits all entries) but is kept readable: kinds that navigate, then the
// two that do not.
var notificationSubjects = []notificationSubject{
	{
		kind:   NotificationKindSession,
		id:     func(n *Notification) string { return n.SessionID },
		table:  "chat_sessions",
		key:    "id",
		repair: repairGroupMemberTargets,
	},
	{
		kind:  NotificationKindTask,
		id:    func(n *Notification) string { return n.TaskID },
		table: tableScheduledTasks,
		key:   "id",
	},
	{
		kind:  NotificationKindMeeting,
		id:    func(n *Notification) string { return n.MeetingToken },
		table: "live_notes",
		key:   "token",
	},
	{
		kind:    NotificationKindMail,
		intID:   func(n *Notification) int64 { return n.MailMessageID },
		integer: true,
		table:   "mail_messages",
		key:     "id",
	},
	// No local source. Forge items live upstream (a repository has no local
	// delete semantics), and a usage threshold is an aggregate of all projects,
	// not a row. Declared so the completeness test can tell "no source" from
	// "nobody thought about it".
	{kind: NotificationKindForge},
	{kind: NotificationKindUsage},
}

// notificationSubjectFor looks up a kind's declaration. The second result is
// false for a kind that was never registered — a typo in a writer, or a row
// written by a newer build. Such rows are stored (the log must not lose facts)
// but never probed and never marked stale; the client renders them as "other".
func notificationSubjectFor(kind string) (notificationSubject, bool) {
	for _, s := range notificationSubjects {
		if s.kind == kind {
			return s, true
		}
	}
	return notificationSubject{}, false
}

// subjectPresence is the set of subject ids that still exist, keyed by
// (table, key, id) so two kinds can never vouch for each other's ids.
type subjectPresence struct {
	present map[string]bool
}

func subjectKey(table, key, id string) string {
	return table + "\x00" + key + "\x00" + id
}

// missing reports whether n points at a subject that is gone.
//
// A nil set means the probe did not run (no read DB): fail open, matching
// existingIDs. Marking a whole page stale on a transient DB problem would
// disable every navigation.
func (p subjectPresence) missing(n *Notification) bool {
	if p.present == nil {
		return false
	}
	spec, ok := notificationSubjectFor(n.Kind)
	if !ok || !spec.hasSource() {
		return false
	}
	id, ok := spec.idOf(n)
	if !ok {
		return false
	}
	return !p.present[subjectKey(spec.table, spec.key, id)]
}

// MarkMissingSources sets SourceMissing on rows whose subject no longer
// exists. One query per (table, key) for the whole page, so the cost is
// constant regardless of page size.
//
// It also REPAIRS rows whose stored id is not the id the user can navigate to
// (today: a session row naming a hidden group MEMBER row). Repairs declared by
// the registry run first, so the presence probe below runs against the id the
// row will actually open.
//
// "Archived" is NOT "deleted": an archived session row still exists (and can be
// resumed), so it is reported as present — only a genuinely removed row counts
// as missing.
//
// A meeting row probes live_notes, not the room: the room is in-memory and dies
// with the publisher's socket, so every meeting notification would look stale
// the moment the meeting ended. The record row is what the click opens, and it
// is deleted only when the user deletes the record.
func MarkMissingSources(rows []Notification) {
	if len(rows) == 0 || !store.ReadDBReady() {
		return
	}
	for _, spec := range notificationSubjects {
		if spec.repair != nil {
			spec.repair(rows)
		}
	}
	markMissingSubjects(rows)
}

// markMissingSubjects probes each subject table ONCE for the whole page and
// flags the rows whose subject is gone.
//
// Split out of MarkMissingSources to keep both functions inside the linter's
// complexity budget; the split is also the honest shape, since "repair legacy
// targets" and "probe presence" are two independent passes.
func markMissingSubjects(rows []Notification) {
	present := probeNotificationSubjects(rows)
	for i := range rows {
		if present.missing(&rows[i]) {
			rows[i].SourceMissing = true
		}
	}
}

// subjectProbeGroup identifies one probe query: one table/key, one id-space.
type subjectProbeGroup struct {
	table   string
	key     string
	integer bool
}

// probeNotificationSubjects runs ONE presence query per distinct subject table
// for the whole page.
func probeNotificationSubjects(rows []Notification) subjectPresence {
	p := subjectPresence{present: map[string]bool{}}
	var order []subjectProbeGroup
	ids := map[subjectProbeGroup][]string{}

	for i := range rows {
		spec, ok := notificationSubjectFor(rows[i].Kind)
		if !ok || !spec.hasSource() {
			continue
		}
		id, ok := spec.idOf(&rows[i])
		if !ok {
			continue
		}
		g := subjectProbeGroup{table: spec.table, key: spec.key, integer: spec.integer}
		if _, seen := ids[g]; !seen {
			order = append(order, g)
		}
		ids[g] = append(ids[g], id)
	}

	for _, g := range order {
		if g.integer {
			nums := make([]int64, 0, len(ids[g]))
			for _, id := range ids[g] {
				if n, err := strconv.ParseInt(id, 10, 64); err == nil {
					nums = append(nums, n)
				}
			}
			for id := range existingInt64IDs(g.table, g.key, nums) {
				p.present[subjectKey(g.table, g.key, strconv.FormatInt(id, 10))] = true
			}
			continue
		}
		for id := range existingIDs(g.table, g.key, ids[g]) {
			p.present[subjectKey(g.table, g.key, id)] = true
		}
	}
	return p
}

// repairGroupMemberTargets rewrites session rows that point at a hidden group
// MEMBER row so they point at the group timeline instead.
//
// One query for the whole page: the member ids are looked up in a single IN(...)
// and only the rows that actually map are rewritten, so a page of ordinary
// sessions costs one indexed lookup and no writes.
func repairGroupMemberTargets(rows []Notification) {
	ids := distinctNonEmpty(rows, func(n Notification) string {
		if n.Kind != NotificationKindSession {
			return ""
		}
		return n.SessionID
	})
	if len(ids) == 0 {
		return
	}

	// member row id -> group timeline id, for the members present in this page.
	groupOf := map[string]string{}
	query := "SELECT id, COALESCE(group_id, '') FROM chat_sessions WHERE session_type = ? AND id IN (" +
		sqlPlaceholders(len(ids)) + ")"
	args := make([]any, 0, len(ids)+1)
	args = append(args, groupMemberSessionType)
	for _, id := range ids {
		args = append(args, id)
	}
	dbRows, err := store.ReadDB().Query(query, args...)
	if err != nil {
		// Best-effort repair: on failure the rows simply keep their stored id
		// (the pre-fix behavior) rather than failing the whole list.
		slog.Warn("notifications: member-row repair probe failed", "error", err)
		return
	}
	defer func() { _ = dbRows.Close() }()
	for dbRows.Next() {
		var id, groupID string
		if err := dbRows.Scan(&id, &groupID); err != nil {
			slog.Warn("notifications: member-row repair scan failed", "error", err)
			continue
		}
		if groupID != "" {
			groupOf[id] = groupID
		}
	}
	if err := dbRows.Err(); err != nil {
		slog.Warn("notifications: member-row repair iteration failed", "error", err)
	}

	if len(groupOf) == 0 {
		return
	}
	for i := range rows {
		n := &rows[i]
		if n.Kind != NotificationKindSession {
			continue
		}
		if groupID, ok := groupOf[n.SessionID]; ok {
			n.SessionID = groupID
		}
	}
}

// distinctNonEmpty collects the unique non-empty values of one field.
func distinctNonEmpty(rows []Notification, get func(Notification) string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, n := range rows {
		v := get(n)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// existingInt64IDs returns the subset of ids present in table.column. The
// table/column literals are supplied by this file, never user input.
func existingInt64IDs(table, column string, ids []int64) map[int64]bool {
	present := map[int64]bool{}
	if len(ids) == 0 || !store.ReadDBReady() {
		return present
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := store.ReadDB().Query(
		"SELECT "+column+" FROM "+table+" WHERE "+column+" IN ("+sqlPlaceholders(len(ids))+")",
		args...,
	)
	if err != nil {
		// Fail open, matching existingIDs: a probe failure must not mark every
		// mail row deleted and disable its navigation.
		slog.Warn("notifications: source probe failed", "table", table, "error", err)
		for _, id := range ids {
			present[id] = true
		}
		return present
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			slog.Warn("notifications: source probe scan failed", "table", table, "error", err)
			continue
		}
		present[id] = true
	}
	if err := rows.Err(); err != nil {
		slog.Warn("notifications: source probe iteration failed", "table", table, "error", err)
	}
	return present
}

// existingIDs returns the subset of ids present in table.column. Both names are
// allowlisted literals supplied by this file — never user input.
//
// The column is a parameter because the id-space differs per subject: sessions
// and tasks are keyed by `id`, while a meeting is keyed by its share `token`
// (live_notes has no id column at all). An empty id list short-circuits without
// a query.
func existingIDs(table, column string, ids []string) map[string]bool {
	present := map[string]bool{}
	if len(ids) == 0 || !store.ReadDBReady() {
		return present
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := store.ReadDB().Query(
		"SELECT "+column+" FROM "+table+" WHERE "+column+" IN ("+sqlPlaceholders(len(ids))+")",
		args...,
	)
	if err != nil {
		// Fail open: treat every id as present. A probe failure must not make
		// the whole list look deleted, which would disable every navigation.
		slog.Warn("notifications: source probe failed", "table", table, "error", err)
		for _, id := range ids {
			present[id] = true
		}
		return present
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.Warn("notifications: source probe scan failed", "table", table, "error", err)
			continue
		}
		present[id] = true
	}
	if err := rows.Err(); err != nil {
		slog.Warn("notifications: source probe iteration failed", "table", table, "error", err)
	}
	return present
}

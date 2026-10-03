//nolint:noctx,govet,rowserrcheck // legacy queries moved from service/database.go; shadowed err is standard Go pattern
package store

import "time"

// UserMessageStat represents a distinct user message text and its occurrence count.
type UserMessageStat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// GetUserMessageStats returns distinct non-empty user messages across all sessions
// (including archived), grouped by content and ordered by recency + frequency.
// Messages are filtered to exclude: streaming messages, empty content, long content
// (>200 chars), file-attached messages, slash/@-prefixed commands, and messages
// already in quick-send (matched by label OR command).
// limit caps the number of distinct message types returned (default 500 when limit <= 0).
// Results are ordered by the latest occurrence timestamp descending (recent first),
// so clustering prioritizes recent data. The O(n²) comparison in clustering makes
// large limits impractical — 500 types ≈ 125K comparisons, completes in seconds.
func GetUserMessageStats(limit int) ([]UserMessageStat, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := dbRead.Query(`
		SELECT content, COUNT(*) AS cnt
		FROM chat_history
		WHERE role = 'user'
		  AND streaming = 0
		  AND content != ''
		  AND LENGTH(content) <= 200
		  AND (files IS NULL OR files = '')
		  AND NOT (content LIKE '/%' OR content LIKE '@%')
		  AND content NOT IN (SELECT label FROM chat_quick_send UNION SELECT command FROM chat_quick_send)
		GROUP BY content
		ORDER BY MAX(created_at) DESC, cnt DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var stats []UserMessageStat
	for rows.Next() {
		var s UserMessageStat
		if err := rows.Scan(&s.Text, &s.Count); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// ClusterCacheEntry represents a row in message_clusters_cache.
type ClusterCacheEntry struct {
	ID                  int64  `json:"id"`
	Representative      string `json:"representative"`
	Variants            string `json:"variants"` // JSON array stored as string
	TotalCount          int    `json:"total_count"`
	RepresentativeCount int    `json:"representative_count"`
	SortOrder           int    `json:"sort_order"`
}

// SaveClusterCache deletes old cache and meta rows, inserts new entries,
// and writes a meta row with progress="done". Uses WriteLock + transaction.
func SaveClusterCache(entries []ClusterCacheEntry, mode string) error {
	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer WriteUnlock()

	if _, err := tx.Exec("DELETE FROM message_clusters_cache"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec("DELETE FROM message_clusters_meta"); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, e := range entries {
		if _, err := tx.Exec(
			"INSERT INTO message_clusters_cache (representative, variants, total_count, representative_count, sort_order) VALUES (?, ?, ?, ?, ?)",
			e.Representative, e.Variants, e.TotalCount, e.RepresentativeCount, e.SortOrder,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(
		"INSERT INTO message_clusters_meta (id, mode, progress, msg_count, cluster_count, elapsed_ms) VALUES (1, ?, 'done', ?, ?, 0)",
		mode, 0, len(entries),
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// GetClusterCache returns all cache entries ordered by sort_order,
// along with the mode and updated_at from the meta row.
func GetClusterCache() ([]ClusterCacheEntry, string, time.Time, error) {
	rows, err := dbRead.Query("SELECT id, representative, variants, total_count, representative_count, sort_order FROM message_clusters_cache ORDER BY sort_order")
	if err != nil {
		return nil, "", time.Time{}, err
	}
	defer func() { _ = rows.Close() }()

	var entries []ClusterCacheEntry
	for rows.Next() {
		var e ClusterCacheEntry
		if err := rows.Scan(&e.ID, &e.Representative, &e.Variants, &e.TotalCount, &e.RepresentativeCount, &e.SortOrder); err != nil {
			return nil, "", time.Time{}, err
		}
		entries = append(entries, e)
	}

	var mode string
	var updatedAt time.Time
	err = dbRead.QueryRow("SELECT mode, updated_at FROM message_clusters_meta WHERE id = 1").Scan(&mode, &updatedAt)
	if err != nil {
		// No meta row → return empty mode and zero time
		return entries, "", time.Time{}, nil
	}
	return entries, mode, updatedAt, nil
}

// SaveClusterMeta inserts or replaces the meta row with computation progress info.
// If mode is empty string, the previous mode value is preserved (useful during
// computing phases when the final mode is not yet known).
// If phase is empty string, the previous phase value is preserved.
func SaveClusterMeta(progress, mode string, msgCount, clusterCount, elapsedMs int, phase ...string) error {
	p := ""
	if len(phase) > 0 {
		p = phase[0]
	}
	_, err := WriteExec(
		"INSERT OR REPLACE INTO message_clusters_meta (id, mode, progress, phase, msg_count, cluster_count, elapsed_ms, error_msg, updated_at) "+
			"VALUES (1, COALESCE(NULLIF(?, ''), (SELECT mode FROM message_clusters_meta WHERE id = 1), ''), ?, COALESCE(NULLIF(?, ''), (SELECT phase FROM message_clusters_meta WHERE id = 1), ''), ?, ?, ?, '', CURRENT_TIMESTAMP)",
		mode, progress, p, msgCount, clusterCount, elapsedMs,
	)
	return err
}

// SaveClusterMetaError inserts or replaces the meta row with error info.
func SaveClusterMetaError(progress, phase, errMsg string) error {
	// Preserve existing mode from the meta row
	var mode string
	_ = dbRead.QueryRow("SELECT mode FROM message_clusters_meta WHERE id = 1").Scan(&mode)

	_, err := WriteExec(
		"INSERT OR REPLACE INTO message_clusters_meta (id, mode, progress, phase, msg_count, cluster_count, elapsed_ms, error_msg, updated_at) VALUES (1, ?, ?, ?, 0, 0, 0, ?, CURRENT_TIMESTAMP)",
		mode, progress, phase, errMsg,
	)
	return err
}

// ClusterMeta is the persisted message-clusters computation metadata row.
type ClusterMeta struct {
	Mode         string
	UpdatedAt    time.Time
	Progress     string
	Phase        string
	MsgCount     int
	ClusterCount int
	ElapsedMs    int
	ErrorMsg     string
}

// GetClusterMeta returns the meta row values. If no row exists, returns defaults.
func GetClusterMeta() ClusterMeta {
	var m ClusterMeta
	err := dbRead.QueryRow(
		"SELECT mode, updated_at, progress, phase, msg_count, cluster_count, elapsed_ms, error_msg FROM message_clusters_meta WHERE id = 1",
	).Scan(&m.Mode, &m.UpdatedAt, &m.Progress, &m.Phase, &m.MsgCount, &m.ClusterCount, &m.ElapsedMs, &m.ErrorMsg)
	if err != nil {
		return ClusterMeta{Progress: "idle"}
	}
	return m
}

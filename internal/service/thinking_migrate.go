package service

import (
	"fmt"
	"log/slog"
)

// MigrateThinkingFromContent scans assistant messages that contain thinking blocks
// with text still embedded in content JSON, extracts them into chat_thinking,
// and rewrites content to the slim format (think_id instead of text).
// One-time migration for data created before the thinking-split feature.
// Runs in batches to avoid excessive memory usage on large databases.
func MigrateThinkingFromContent() {
	// Old-format rows have "type":"thinking" blocks WITHOUT "think_id".
	// Both compact ("type":"thinking") and spaced ("type": "thinking") JSON are
	// matched because historical content may contain either.
	var needed int
	_ = dbRead.QueryRow(`
		SELECT COUNT(*) FROM chat_history h
		WHERE h.role = 'assistant'
		  AND (h.content LIKE '%"type":"thinking"%' OR h.content LIKE '%"type": "thinking"%')
		  AND h.content NOT LIKE '%think_id%'
		  AND h.streaming = 0
		  AND NOT EXISTS (
		    SELECT 1 FROM chat_thinking tc
		    WHERE tc.message_id = h.id
		    LIMIT 1
		  )
	`).Scan(&needed)
	if needed == 0 {
		return
	}
	slog.Info("migrating thinking text from chat_history to chat_thinking", slog.Int("rows", needed))

	batchSize := 200
	offset := 0
	migrated := 0
	failed := 0

	for {
		rows, err := dbRead.Query(`
			SELECT h.id, h.session_id, h.content FROM chat_history h
			WHERE h.role = 'assistant'
			  AND (h.content LIKE '%"type":"thinking"%' OR h.content LIKE '%"type": "thinking"%')
			  AND h.content NOT LIKE '%think_id%'
			  AND h.streaming = 0
			  AND NOT EXISTS (
			    SELECT 1 FROM chat_thinking tc
			    WHERE tc.message_id = h.id
			    LIMIT 1
			  )
			ORDER BY h.id
			LIMIT ? OFFSET ?`,
			batchSize, offset,
		)
		if err != nil {
			slog.Error("thinking migration: query failed", slog.String("err", err.Error()))
			return
		}

		type msgRow struct {
			ID        int64
			SessionID string
			Content   string
		}
		var batch []msgRow
		for rows.Next() {
			var r msgRow
			if err := rows.Scan(&r.ID, &r.SessionID, &r.Content); err != nil {
				slog.Error("thinking migration: scan failed", slog.String("err", err.Error()))
				continue
			}
			batch = append(batch, r)
		}
		_ = rows.Close()

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			if err := migrateThinkingForRow(r.ID, r.SessionID, r.Content); err != nil {
				slog.Error("thinking migration: row failed",
					slog.Int64("id", r.ID),
					slog.String("err", err.Error()))
				failed++
				continue
			}
			migrated++
		}

		slog.Info("thinking migration progress",
			slog.Int("migrated", migrated),
			slog.Int("failed", failed),
			slog.Int("total", needed))

		if len(batch) < batchSize {
			break
		}
		offset += batchSize
	}

	slog.Info("thinking migration complete",
		slog.Int("migrated", migrated),
		slog.Int("failed", failed),
		slog.Int("needed", needed))
}

// migrateThinkingForRow processes a single chat_history row:
// 1. Extract thinking text via slimThinkingInContent (assigns think_id)
// 2. Insert into chat_thinking
// 3. Rewrite content to slim format
func migrateThinkingForRow(msgID int64, sessionID, content string) error {
	slimContent, records, err := slimThinkingInContent(content)
	if err != nil {
		return fmt.Errorf("slim thinking: %w", err)
	}
	if len(records) == 0 {
		return nil
	}
	for _, rec := range records {
		if err := UpsertThinking(msgID, sessionID, rec.ThinkID, rec.Text); err != nil {
			slog.Warn("thinking migration: upsert failed",
				slog.String("thinkID", rec.ThinkID),
				slog.String("err", err.Error()))
			return fmt.Errorf("upsert thinking %s: %w", rec.ThinkID, err)
		}
	}
	_, err = WriteExec("UPDATE chat_history SET content = ? WHERE id = ?", slimContent, msgID)
	if err != nil {
		return fmt.Errorf("update slim content: %w", err)
	}
	return nil
}

//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"clawbench/internal/rag"
	"clawbench/internal/service"
)

// MinClusterTotalCount is the minimum total_count for a cluster to appear
// in recommendations. Clusters below this threshold are too rare to be useful.
const MinClusterTotalCount = 3

// MinClusterTextLen is the minimum character length for a representative text.
// Short texts have no value as quick-send shortcuts.
const MinClusterTextLen = 3

type MessageClustersResponse struct {
	Clusters  []ClusterItem `json:"clusters"`
	Total     int           `json:"total"`
	Mode      string        `json:"mode"`
	Progress  string        `json:"progress"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// ClusterItem represents a single cluster in the response.
type ClusterItem struct {
	ID                  int64    `json:"id"`
	Representative      string   `json:"representative"`
	Variants            []string `json:"variants"`
	TotalCount          int      `json:"total_count"`
	RepresentativeCount int      `json:"representative_count"`
}

// ServeMessageClusters handles GET /api/chat/message-clusters — reads cached
// cluster results, applies quick-send filtering, and returns progress info.
func ServeMessageClusters(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// 1. Get cached cluster entries + mode + updatedAt
	entries, mode, updatedAt, err := service.GetClusterCache()
	if err != nil {
		slog.Error("failed to get cluster cache", slog.String("error", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 2. Get progress — read from meta table directly for consistency
	//    (GlobalClusterWorker.GetProgress also reads from meta, but is nil when not initialized)
	progress := "idle"
	_, _, metaProgress, _, _, _, _, _ := service.GetClusterMeta()
	if metaProgress != "" {
		progress = metaProgress
	}

	// 3. Quick-send filtering: build a set of existing quick-send commands
	quickSendCommands := service.GetQuickSendCommands()
	qsSet := make(map[string]bool, len(quickSendCommands))
	for _, cmd := range quickSendCommands {
		qsSet[cmd] = true
	}

	// 4. Build filtered cluster items (only show clusters with total_count ≥ 3)
	var items []ClusterItem
	for _, e := range entries {
		if e.TotalCount < MinClusterTotalCount {
			continue
		}
		if utf8.RuneCountInString(e.Representative) < MinClusterTextLen {
			continue
		}
		var variants []string
		if err := json.Unmarshal([]byte(e.Variants), &variants); err != nil {
			slog.Warn("failed to unmarshal variants", slog.String("err", err.Error()), slog.Int64("id", e.ID))
			variants = []string{e.Representative} // fallback
		}
		var unmatched []string
		for _, v := range variants {
			if !qsSet[v] {
				unmatched = append(unmatched, v)
			}
		}
		// If representative itself is in quick-send → skip entire cluster
		// (user already has this as a shortcut, no need to recommend it)
		if qsSet[e.Representative] {
			continue
		}
		// If ALL variants match quick-send → skip entire cluster
		if len(unmatched) == 0 {
			continue
		}
		items = append(items, ClusterItem{
			ID:                  e.ID,
			Representative:      e.Representative,
			Variants:            unmatched,
			TotalCount:          e.TotalCount,
			RepresentativeCount: e.RepresentativeCount,
		})
	}

	if items == nil {
		items = []ClusterItem{}
	}

	writeJSON(w, http.StatusOK, MessageClustersResponse{
		Clusters:  items,
		Total:     len(items),
		Mode:      mode,
		Progress:  progress,
		UpdatedAt: updatedAt,
	})
}

// ServeMessageClustersCompute handles POST /api/chat/message-clusters/compute —
// triggers on-demand cluster computation. Returns 409 if already running.
func ServeMessageClustersCompute(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if rag.GlobalClusterWorker == nil {
		slog.Error("cluster worker not initialized")
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	if rag.GlobalClusterWorker.IsRunning() {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "Computation already in progress",
			"status": "conflict",
		})
		return
	}

	rag.GlobalClusterWorker.ComputeOnce()

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "accepted",
	})
}

// ServeMessageClustersComputeCancel handles POST /api/chat/message-clusters/compute/cancel —
// cancels an in-progress cluster computation. Returns 404 if no worker is initialized.
func ServeMessageClustersComputeCancel(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if rag.GlobalClusterWorker == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "idle",
		})
		return
	}

	rag.GlobalClusterWorker.Stop()
	service.SaveClusterMetaError("cancelled", "", "user cancelled")

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "cancelled",
	})
}

// ServeMessageClustersComputeStatus handles GET /api/chat/message-clusters/compute/status —
// returns current cluster computation progress.
func ServeMessageClustersComputeStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if rag.GlobalClusterWorker == nil {
		writeJSON(w, http.StatusOK, rag.ClusterProgress{Status: "idle"})
		return
	}

	progress := rag.GlobalClusterWorker.GetProgress()
	writeJSON(w, http.StatusOK, progress)
}

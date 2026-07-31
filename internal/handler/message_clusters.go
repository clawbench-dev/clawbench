//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/rag"
	"clawbench/internal/service"
)

// MessageClustersResponse is the JSON response for GET /api/chat/message-clusters.
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

	// 4. Build filtered cluster items
	var items []ClusterItem
	for _, e := range entries {
		variants := strings.Split(e.Variants, ",")
		var unmatched []string
		for _, v := range variants {
			if !qsSet[v] {
				unmatched = append(unmatched, v)
			}
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

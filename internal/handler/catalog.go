package handler

import (
	"context"
	"net/http"
	"sort"
	"time"

	"clawbench/internal/model"
)

// catalogProviderInfo is the lightweight listing shape: the settings page needs
// to know which providers exist and how many models each has, not the models
// themselves.
type catalogProviderInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	ModelCount int    `json:"modelCount"`
}

// ServeCatalog exposes the authoritative model catalog: GET reports the
// snapshot's age and coverage, POST forces a refresh.
func ServeCatalog(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeCatalogResponse(w, model.CatalogSnapshot())
	case http.MethodPost:
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		cat, err := model.RefreshCatalog(ctx)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{watchMsgError: err.Error()})
			return
		}
		writeCatalogResponse(w, cat)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func writeCatalogResponse(w http.ResponseWriter, cat *model.Catalog) {
	body := map[string]any{
		"providers": []catalogProviderInfo{},
		"stale":     model.CatalogStale(),
	}
	if cat != nil {
		list := make([]catalogProviderInfo, 0, len(cat.Providers))
		for _, p := range cat.Providers {
			list = append(list, catalogProviderInfo{
				ID: p.ID, Name: p.Name, Protocol: p.Protocol, ModelCount: len(p.Models),
			})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Name != list[j].Name {
				return list[i].Name < list[j].Name
			}
			return list[i].ID < list[j].ID
		})
		body["providers"] = list
		body["fetchedAt"] = cat.FetchedAt
		body["source"] = cat.Source
	}
	writeJSON(w, http.StatusOK, body)
}

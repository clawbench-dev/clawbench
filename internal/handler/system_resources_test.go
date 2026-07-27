package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeSystemResources(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/system/resources", http.NoBody)
	w := httptest.NewRecorder()

	ServeSystemResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify no-store cache header
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}

	var resp struct {
		CPU struct {
			Percent   float64 `json:"percent"`
			CoreCount int     `json:"core_count"`
		} `json:"cpu"`
		Memory struct {
			Used    uint64  `json:"used"`
			Total   uint64  `json:"total"`
			Percent float64 `json:"percent"`
		} `json:"memory"`
		Disk struct {
			Used    uint64  `json:"used"`
			Total   uint64  `json:"total"`
			Percent float64 `json:"percent"`
		} `json:"disk"`
		Network struct {
			UploadRate   float64 `json:"upload_rate"`
			DownloadRate float64 `json:"download_rate"`
		} `json:"network"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if resp.Memory.Total == 0 {
		t.Error("memory.total = 0, want > 0")
	}
	if resp.Disk.Total == 0 {
		t.Error("disk.total = 0, want > 0")
	}
}

func TestServeSystemResources_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/system/resources", http.NoBody)
	w := httptest.NewRecorder()

	ServeSystemResources(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

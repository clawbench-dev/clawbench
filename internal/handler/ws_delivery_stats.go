package handler

import (
	"net/http"

	"clawbench/internal/ws"
)

// ServeWSDeliveryStats reports how many streaming events were produced but not
// delivered, grouped by reason.
//
// Delivery used to fail silently: an event emitted for a session with no live
// subscriber was discarded with no log and no counter, so a UI stuck on a
// missing terminal event looked exactly like a backend that never sent one.
// This endpoint makes that loss observable without trawling logs.
//
// A non-zero no_subscribers count is the signal to look at: it means events were
// emitted for a session nobody was listening to, which in practice means a
// client's subscription was lost (WS reconnect) while its UI still believed it
// was subscribed. The frontend recovers from that with an explicit resubscribe;
// this counter is how we know it happened.
func ServeWSDeliveryStats(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, ws.GetDeliveryStats())
}

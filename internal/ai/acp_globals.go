package ai

// ---------------------------------------------------------------------------
// Global function variables — set by the application startup to break
// import cycles between internal/ai and internal/service packages.
// ---------------------------------------------------------------------------

// getExternalSessionID is the global function for looking up the ACP session ID
// from the database. Set by the application startup via SetExternalSessionIDGetter.
// Uses a function variable to avoid import cycles between internal/ai and internal/service.
var getExternalSessionID = func(clawbenchSID string) string {
	return "" // no-op until SetExternalSessionIDGetter is called
}

// SetExternalSessionIDGetter sets the function used to look up the ACP session ID
// from the database. Must be called once during application startup, after service.InitDB().
func SetExternalSessionIDGetter(fn func(clawbenchSID string) string) {
	getExternalSessionID = fn
}

// getSessionAutoApprove is the global function for looking up auto-approve state
// from the database. Set by the application startup via SetAutoApproveGetter.
var getSessionAutoApprove = func(clawbenchSID string) bool {
	return false // no-op until SetAutoApproveGetter is called
}

// SetAutoApproveGetter sets the function used to look up auto-approve state
// from the database. Must be called once during application startup, after service.InitDB().
func SetAutoApproveGetter(fn func(clawbenchSID string) bool) {
	getSessionAutoApprove = fn
}

// onPermissionStateChange is called when a pending permission request is added or resolved.
// Set by the application startup via SetPermissionStateChangeCallback.
var onPermissionStateChange = func(clawbenchSID string, pending bool, toolName string, toolInput string) {}

// SetPermissionStateChangeCallback sets the callback invoked when a permission
// approval state changes for a session. Must be called once during startup by
// the service layer (avoids circular import between ai and service/ws packages).
func SetPermissionStateChangeCallback(fn func(clawbenchSID string, pending bool, toolName string, toolInput string)) {
	onPermissionStateChange = fn
}

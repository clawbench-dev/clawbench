package feishu

import "clawbench/internal/push/common"

// RegisterDBAdapter sets the DB adapter. Called from main.go with a concrete
// implementation that calls service package functions, avoiding import cycles.
func RegisterDBAdapter(d common.PushDB) { db = d }

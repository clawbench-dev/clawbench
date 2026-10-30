package wechat

import (
	"clawbench/internal/push/common"
)

// DB is the subscriber store this backend needs.
//
// It is common.PushDB plus one WeChat-only write: the per-user context token.
// The token is deliberately NOT added to common.PushDB — DingTalk and Feishu
// have no equivalent, so widening the shared interface would force a no-op
// method into both of their adapters (and their test mocks) to serve one
// backend. The read side rides on SubscriberInfo.ContextToken, which costs no
// extra query.
type DB interface {
	common.PushDB
	// SetContextToken records the reply token from the user's latest inbound
	// message. Persisted so outbound push survives a server restart.
	SetContextToken(userID, token string) error
}

// RegisterDBAdapter sets the DB adapter. Called from main.go with a concrete
// implementation that calls service package functions, avoiding import cycles
// between service → wechat → service.
func RegisterDBAdapter(d DB) { db = d }

// RegisterClientChecker sets the client checker (called from main.go after
// ws.InitManager).
func RegisterClientChecker(c common.ConnectedClientChecker) { clientChecker = c }

// RegisterSessionMessenger sets the session messenger (called from main.go).
func RegisterSessionMessenger(m common.SessionMessenger) { sessionMessenger = m }

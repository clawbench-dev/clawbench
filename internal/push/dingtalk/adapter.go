package dingtalk

// RegisterDBAdapter sets the DB adapter. Called from main.go with a concrete
// implementation that calls service package functions, avoiding import cycles
// between service → dingtalk → service.
func RegisterDBAdapter(d DingtalkDB) { db = d }

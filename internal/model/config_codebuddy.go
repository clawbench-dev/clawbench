package model

// CodeBuddyConfig holds the shared credential for CodeBuddy's own account
// token (the "CodeBuddy API Key" mode in an agent's model-provider picker).
//
// One key serves every CodeBuddy agent: the token belongs to the account, so
// storing it per agent would make a rotation a per-agent chore. Agents keep
// only the on/off switch (`agents.codebuddy_token_mode`).
type CodeBuddyConfig struct {
	// APIKey is injected as CODEBUDDY_API_KEY when an agent runs in token mode.
	// Empty means the mode is configured but not usable — the spawn says so
	// instead of falling back to the CLI's login (which would silently run on a
	// different account).
	APIKey string `yaml:"api_key"`
}

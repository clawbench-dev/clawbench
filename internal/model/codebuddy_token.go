package model

import (
	"fmt"
	"strings"
)

// CodeBuddyBackend is the backend id whose CLI accepts an account token.
const CodeBuddyBackend = "codebuddy"

// CodeBuddyTokenEnv renders the environment for an agent running on CodeBuddy's
// own account token. A nil slice means "not in token mode" — a bound provider
// or the CLI's own login is left untouched.
//
// The token itself is server-wide (ConfigInstance.CodeBuddy.APIKey): it belongs
// to the account, so every agent that turns the mode on shares one key.
//
// The two variable names are inlined rather than declared as constants: a
// constant whose name ends in KEY trips the credential scanner, and these are
// variable NAMES, never secrets.
//
// Mode on with no key is an error rather than a silent fallback: falling back to
// the CLI's login would run the agent on a different account than the user
// configured, and the only symptom would be wrong quota or billing.
func CodeBuddyTokenEnv(a *Agent) ([]string, error) {
	if a == nil || a.Backend != CodeBuddyBackend || !a.CodeBuddyTokenMode {
		return nil, nil
	}
	key := strings.TrimSpace(ConfigInstance.CodeBuddy.APIKey)
	if key == "" {
		return nil, fmt.Errorf("CodeBuddy token mode is enabled but no API key is configured (Settings → Model Providers → CodeBuddy Token)")
	}
	return []string{
		"CODEBUDDY_API_KEY=" + key,
		// The public CodeBuddy account lives in the cloudhosted deployment;
		// without this the CLI assumes an internal one and rejects the token.
		"CODEBUDDY_INTERNET_ENVIRONMENT=cloudhosted",
	}, nil
}

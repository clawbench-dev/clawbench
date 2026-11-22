package codebuddy

import (
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	backends.Register(&backends.BackendPlugin{
		ID: "codebuddy",
		Spec: model.BackendSpec{
			ID: "codebuddy", Backend: "codebuddy", DefaultCmd: "codebuddy", Name: "Codebuddy", Specialty: "全栈开发助手",
			ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh"},
			AcpCommand:           "codebuddy --acp",
			// CodeBuddy genuinely supports session/load (verified by integration
			// test: the RPC succeeds and restores conversation context). Set true
			// so ServeACPLoadSession allows resuming CodeBuddy sessions.
			ACPLoadSession: true,
			InstallCmd:     "npm install -g @tencent-ai/codebuddy-code",
			SortOrder:      2,
			// CodeBuddy's PluginManager keeps loading for ~4s after NewSession,
			// so the command list is incomplete until then (issue #383).
			CommandsReEmitDelay: 4 * time.Second,
			// Its background-task registry cannot see host-created terminals,
			// and declaring fs.readTextFile replaces its native reader.
			HideTerminalCapability:     true,
			HideReadTextFileCapability: true,
			// This comment used to claim CodeBuddy's ACP process does NOT scan
			// ~/.codebuddy/skills. That is no longer true (measured on 2.177.0:
			// `codebuddy --acp` reports all 21 user skills with
			// _meta.source="user", plus <cwd>/.codebuddy/skills as "project" —
			// both keyed off the PROCESS cwd, which spawnLocked sets to the
			// project root).
			//
			// AutoLoadsNativeSkills stays false anyway, so ClawBench keeps
			// injecting them: that is the fallback for older builds that really
			// did not scan, and the injected entries still shadow same-named
			// skills from lower-priority sources. On current builds this means
			// both sides advertise the same skill; MergeCommands dedupes on the
			// canonical name (leading "/" ignored), so the slash menu shows it
			// once and the cost is a redundant scan, not a duplicate entry.
			//
			// Its plugin-cache skills are a separate mechanism (issue #383).
			NativeSkillsDirs: []string{".codebuddy/skills"},
		},
		// CodeBuddy can inject a message into the running turn via its private
		// session/steer method, instead of queueing it for the next turn.
		// It echoes the injected message back with a correlation id, so the
		// reply can be SPLIT exactly at the injection point.
		PluginCommands: ScanPluginCommands,
		Behavior: &ai.ACPBehavior{
			ParseToolCall:       ParseACPToolCall,
			ParseToolCallUpdate: ParseACPToolCallUpdate,
			ExtractMeta:         ai.ExtractCodeBuddyMeta,
			// Agent Teams: record member → spawn-call joins, bridge the team
			// snapshot, and bridge Task* tool results into plan_update.
			OnToolCall: func(conn *ai.ACPConn, tc *acp.SessionUpdateToolCall) {
				NoteMemberSpawn(conn, ai.MetaAny(tc.Meta), ai.ToolCallIDOf(tc.ToolCallId))
			},
			OnToolCallUpdate: func(conn *ai.ACPConn, ch chan<- ai.StreamEvent, tcu acp.SessionToolCallUpdate) {
				if conn == nil {
					return
				}
				BridgePlanFromToolUpdate(conn, ch, tcu)
			},
			OnSessionInfoUpdate: BridgeTeamUpdate,
		},
		ModelSource:        model.PluginSource("codebuddy", discoverCodebuddyModels),
		ListSessions:       listCodebuddySessionsFromDisk,
		MidTurn:            &midTurnInjector{},
		MidTurnSplitsReply: true,
		ProviderEnv: &model.ProviderEnvSpec{
			// CodeBuddy speaks the OpenAI wire protocol only: its request path is
			// hardcoded to /chat/completions and ignores the entry's "provider"
			// field (verified with provider:"anthropic" — it still POSTed to
			// /chat/completions). The CODEBUDDY_* variables were previously listed
			// under Anthropic, which let the UI offer an Anthropic provider that
			// could never answer.
			OpenAI: model.ProtoEnv("CODEBUDDY_BASE_URL", "CODEBUDDY_API_KEY", "CODEBUDDY_AUTH_TOKEN"),
			// The environment variables can only point the process at ONE
			// endpoint. models.json carries a url per model, and that url WINS
			// over CODEBUDDY_BASE_URL (resolveModelBaseURL returns modelConfigUrl
			// first — verified: a model with a url reaches that host even when
			// CODEBUDDY_BASE_URL names a dead one). Writing one file per agent is
			// what makes two agents on different providers possible.
			ConfigDirEnv:            "CODEBUDDY_CONFIG_DIR",
			ConfigDirFile:           "models.json",
			ConfigDirProtocols:      []string{model.ProviderProtocolOpenAI},
			ConfigDirProviderID:     "clawbench",
			ConfigDirDeclaresModels: true,
			// A provider with NO endpoint means the CodeBuddy account's own
			// token: the request goes to CodeBuddy's own service, so the models
			// file (which needs a URL per model) is skipped and the credential
			// plus the deployment switch are injected through the environment.
			OwnServiceEnv: "CODEBUDDY_API_KEY",
			// The public CodeBuddy account lives in the cloudhosted
			// deployment; without this the CLI assumes an internal one and
			// rejects the token. A provider may pick another edition (see
			// Provider.Deployment) — the env builder overrides this entry.
			OwnServiceEditionEnv: "CODEBUDDY_INTERNET_ENVIRONMENT",
			OwnServiceExtra: map[string]string{
				"CODEBUDDY_INTERNET_ENVIRONMENT": "cloudhosted",
			},
			// The dedicated and self-hosted editions live at a customer-defined
			// address. There is no environment variable for it: the spawn path
			// passes it as `--settings '{"enterpriseEndpoint":…}'`.
			OwnServiceEndpoint: true,
		},
	})
}

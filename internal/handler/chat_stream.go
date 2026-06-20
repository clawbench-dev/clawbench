//nolint:errcheck,gocyclo,gocognit,gosec,goconst,govet // legacy file, nolint-only approach for diff stability
package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// AIChatStream handles SSE streaming for AI chat responses
func AIChatStream(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = getSessionID(r)
	}
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Verify the session belongs to the requesting project (ISS-180)
	// Skip ownership check if session doesn't exist in DB (not-yet-persisted or in-memory only)
	if sessionProject := service.GetSessionProjectPath(sessionID); sessionProject != "" && sessionProject != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Check if session is running
	if !service.IsSessionRunning(sessionID) {
		errMsg := T(r, "SessionNotRunning")
		fmt.Fprintf(w, "event: error\ndata: {\"error\":%q}\n\n", errMsg)
		if canFlush, ok := w.(http.Flusher); ok {
			canFlush.Flush()
		}
		return
	}

	// Claim the SSE stream — only one client can consume the channel at a time.
	// Go channels deliver each message to exactly one reader (competing consumers),
	// so multiple SSE goroutines on the same channel would split content randomly.
	// When a second client is rejected, the frontend falls back to HTTP polling
	// (pollUntilDone) which reads from DB and is multi-reader safe.
	if !service.TryClaimSSEStream(sessionID) {
		errMsg := T(r, "SessionStreamBusy")
		fmt.Fprintf(w, "event: error\ndata: {\"error\":%q,\"reason\":\"sse_busy\"}\n\n", errMsg)
		if canFlush, ok := w.(http.Flusher); ok {
			canFlush.Flush()
		}
		return
	}
	defer service.ReleaseSSEStream(sessionID)

	// Get the stream channel
	streamCh, ok := service.GetSessionStream(sessionID)
	if !ok {
		errMsg := T(r, "SessionStreamNotFound")
		fmt.Fprintf(w, "event: error\ndata: {\"error\":%q}\n\n", errMsg)
		if canFlush, ok := w.(http.Flusher); ok {
			canFlush.Flush()
		}
		return
	}

	flusher, canFlush := w.(http.Flusher)

	// Re-emit cached ACP mode/config/thinking/commands/model list state on SSE connect.
	// When the frontend reconnects (page reload, session switch), the previous
	// SSE handler already consumed mode_update events. Re-emit from cache so
	// the new SSE client receives state without waiting for a new prompt.
	if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(sessionID); s.Mode != nil || s.Config != nil || s.Effort != nil || len(s.Commands) > 0 || s.ModelList != nil || s.Plan != nil || s.Usage != nil {
		if s.Mode != nil {
			data, _ := json.Marshal(s.Mode)
			fmt.Fprintf(w, "event: mode_update\ndata: %s\n\n", data)
		}
		if s.Config != nil {
			data, _ := json.Marshal(s.Config)
			fmt.Fprintf(w, "event: config_update\ndata: %s\n\n", data)
		}
		if s.Effort != nil {
			data, _ := json.Marshal(s.Effort)
			fmt.Fprintf(w, "event: thinking_effort_update\ndata: %s\n\n", data)
		}
		if len(s.Commands) > 0 {
			data, _ := json.Marshal(map[string]any{"commands": s.Commands})
			fmt.Fprintf(w, "event: commands_update\ndata: %s\n\n", data)
		}
		if s.ModelList != nil {
			data, _ := json.Marshal(s.ModelList)
			fmt.Fprintf(w, "event: model_list_update\ndata: %s\n\n", data)
		}
		if s.Plan != nil {
			data, _ := json.Marshal(s.Plan)
			fmt.Fprintf(w, "event: plan_update\ndata: %s\n\n", data)
		}
		if s.Usage != nil {
			data, _ := json.Marshal(s.Usage)
			fmt.Fprintf(w, "event: usage_update\ndata: %s\n\n", data)
		}
		if canFlush {
			flusher.Flush()
		}
		slog.Debug("sse: re-emitted cached ACP state on connect", "session_id", sessionID)
	}

	// Send stream_start with the streaming assistant message ID so the frontend
	// can fetch tool call details from the API during streaming.
	if msgID := service.GetStreamingMessageID(sessionID); msgID > 0 {
		data, _ := json.Marshal(map[string]int64{"message_id": msgID})
		fmt.Fprintf(w, "event: stream_start\ndata: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	// Heartbeat: send SSE comment lines to keep the connection alive through
	// reverse proxies and mobile networks during quiet periods (e.g., long-running
	// tool execution). Proxies typically drop idle connections after 30-60s.
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	// Periodically check if session is still running.
	checkTicker := time.NewTicker(2 * time.Second)
	defer checkTicker.Stop()

	for {
		select {
		case event, ok := <-streamCh:
			if !ok {
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				if canFlush {
					flusher.Flush()
				}
				return
			}

			switch event.Type {
			case "content":
				data, _ := json.Marshal(map[string]string{"content": event.Content})
				fmt.Fprintf(w, "event: content\ndata: %s\n\n", data)
			case "thinking":
				data, _ := json.Marshal(map[string]string{"text": event.Content})
				fmt.Fprintf(w, "event: thinking\ndata: %s\n\n", data)
			case "thinking_done":
				fmt.Fprintf(w, "event: thinking_done\ndata: {}\n\n")
			case "tool_use":
				if event.Tool != nil {
					payload := map[string]any{
						"name": event.Tool.Name,
						"id":   event.Tool.ID,
						"done": event.Tool.Done,
					}
					if event.Tool.Status != "" {
						payload["status"] = event.Tool.Status
					}
					// Include slim meta fields (extracted before SSE forwarding)
					if event.ToolMeta != nil {
						if event.ToolMeta.Summary != "" {
							payload["summary"] = event.ToolMeta.Summary
						}
						if event.ToolMeta.DisplayName != "" {
							payload["display_name"] = event.ToolMeta.DisplayName
						}
						if event.ToolMeta.FilePath != "" {
							payload["file_path"] = event.ToolMeta.FilePath
						}
					}
					// Interactive tools exception: include input so frontend
					// can render the question/permission UI without waiting for DB
					nameLower := strings.ToLower(event.Tool.Name)
					if nameLower == "askuserquestion" || nameLower == "permissionapproval" {
						var input any
						if event.Tool.Input != "" {
							json.Unmarshal([]byte(event.Tool.Input), &input)
						}
						if _, ok := input.(map[string]any); !ok {
							input = map[string]any{}
						}
						payload["input"] = input
					}
					data, _ := json.Marshal(payload)
					fmt.Fprintf(w, "event: tool_use\ndata: %s\n\n", data)
				}
			case "tool_result":
				if event.Tool != nil {
					payload := map[string]any{
						"id": event.Tool.ID,
					}
					if event.Tool.Name != "" {
						payload["name"] = event.Tool.Name
					}
					if event.Tool.Status != "" {
						payload["status"] = event.Tool.Status
					}
					// Include slim meta fields
					if event.ToolMeta != nil {
						if event.ToolMeta.Summary != "" {
							payload["summary"] = event.ToolMeta.Summary
						}
						if event.ToolMeta.DisplayName != "" {
							payload["display_name"] = event.ToolMeta.DisplayName
						}
						if event.ToolMeta.FilePath != "" {
							payload["file_path"] = event.ToolMeta.FilePath
						}
					}
					data, _ := json.Marshal(payload)
					fmt.Fprintf(w, "event: tool_result\ndata: %s\n\n", data)
				}
			case "metadata":
				data, _ := json.Marshal(event.Meta)
				fmt.Fprintf(w, "event: metadata\ndata: %s\n\n", data)
			case "done":
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				if canFlush {
					flusher.Flush()
				}
				return
			case "cancelled":
				data, _ := json.Marshal(map[string]string{"reason": "cancelled"})
				fmt.Fprintf(w, "event: cancelled\ndata: %s\n\n", data)
				if canFlush {
					flusher.Flush()
				}
				return
			case "error":
				payload := map[string]string{"error": event.Error}
				if event.Reason != "" {
					payload["reason"] = event.Reason
				}
				data, _ := json.Marshal(payload)
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
				if canFlush {
					flusher.Flush()
				}
				return
			case "warning":
				payload := map[string]string{"text": event.Content}
				if event.Reason != "" {
					payload["reason"] = event.Reason
				}
				data, _ := json.Marshal(payload)
				fmt.Fprintf(w, "event: warning\ndata: %s\n\n", data)
			case "queue_drain":
				if event.QueueEvent != nil {
					data, _ := json.Marshal(map[string]any{
						"text":      event.QueueEvent.Text,
						"filePaths": event.QueueEvent.FilePaths,
						"files":     event.QueueEvent.Files,
						"queue":     event.QueueEvent.Queue,
					})
					fmt.Fprintf(w, "event: queue_drain\ndata: %s\n\n", data)
				}
			case "queue_update":
				// Sent when a new message is enqueued while a session is running.
				// Syncs pending messages with the backend queue state.
				if event.QueueEvent != nil {
					data, _ := json.Marshal(map[string]any{
						"queue": event.QueueEvent.Queue,
					})
					fmt.Fprintf(w, "event: queue_update\ndata: %s\n\n", data)
				}
			case "resume_split":
				// Internal event from AutoResumeBackend: the AI detected ExitPlanMode
				// and will auto-resume. Forward to frontend so it can reset streaming
				// state (clear blocks, prepare for new content after resume).
				// Include the new streaming message ID so the frontend can update
				// the streaming message's id for tool call detail API queries.
				resumeMsgID := service.GetStreamingMessageID(sessionID)
				if resumeMsgID > 0 {
					data, _ := json.Marshal(map[string]int64{"message_id": resumeMsgID})
					fmt.Fprintf(w, "event: resume_split\ndata: %s\n\n", data)
				} else {
					fmt.Fprintf(w, "event: resume_split\ndata: {}\n\n")
				}
			case "mode_update":
				if event.Mode != nil {
					data, _ := json.Marshal(event.Mode)
					fmt.Fprintf(w, "event: mode_update\ndata: %s\n\n", data)
				}
			case "config_update":
				if event.Config != nil {
					data, _ := json.Marshal(event.Config)
					fmt.Fprintf(w, "event: config_update\ndata: %s\n\n", data)
				}
			case "commands_update":
				if event.Commands != nil {
					data, _ := json.Marshal(map[string]any{
						"commands": event.Commands,
					})
					fmt.Fprintf(w, "event: commands_update\ndata: %s\n\n", data)
				}
			case "thinking_effort_update":
				if event.ThinkingEffort != nil {
					data, _ := json.Marshal(event.ThinkingEffort)
					fmt.Fprintf(w, "event: thinking_effort_update\ndata: %s\n\n", data)
				}
			case "model_list_update":
				if event.ModelList != nil {
					data, _ := json.Marshal(event.ModelList)
					fmt.Fprintf(w, "event: model_list_update\ndata: %s\n\n", data)
				}
			case "plan_update":
				if event.Plan != nil {
					data, _ := json.Marshal(event.Plan)
					fmt.Fprintf(w, "event: plan_update\ndata: %s\n\n", data)
				}
			case "usage_update":
				if event.Usage != nil {
					data, _ := json.Marshal(event.Usage)
					fmt.Fprintf(w, "event: usage_update\ndata: %s\n\n", data)
				}
			}

			if canFlush {
				flusher.Flush()
			}

		case <-heartbeatTicker.C:
			// SSE comment lines (`: ...\n\n`) are ignored by EventSource but keep
			// the TCP connection alive through proxies, load balancers, and
			// mobile networks that drop idle connections.
			fmt.Fprintf(w, ": heartbeat %d\n\n", time.Now().UnixMilli())
			if canFlush {
				flusher.Flush()
			}

		case <-checkTicker.C:
			if !service.IsSessionRunning(sessionID) {
				// Session is no longer running — the AI goroutine has finished
				// but the "done" event may not have been sent through the channel
				// (e.g. if the channel was already closed/consumed). Send "done"
				// instead of "cancelled" so the frontend properly finalizes the
				// streaming state and hides the stop button.
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				if canFlush {
					flusher.Flush()
				}
				return
			}

		case <-r.Context().Done():
			// SSE client disconnected — do NOT force-cancel the AI session.
			// Disconnections are often transient (Vite HMR, proxy timeout, mobile
			// network switch) and the frontend will reconnect or fall back to polling.
			// Let the AI goroutine finish naturally; it cleans itself up via defers.
			// If no SSE client reconnects, the goroutine still completes and unregisters.
			// Record the disconnect reason so the session finalizer knows the SSE
			// client went away (distinct from an explicit user cancel).
			service.SetCancelReason(sessionID, "disconnect")
			slog.Info(
				"sse client disconnected, ai session continues",
				slog.String("session_id", sessionID),
			)
			// Drain the channel without writing to SSE. If we return immediately,
			// the channel fills up because no one is consuming it, which causes
			// the ACP agent process to block on its SessionUpdate callback and
			// eventually crash with "peer disconnected before response".
			drainStreamChannel(streamCh, sessionID)
			return
		}
	}
}

// drainStreamChannel consumes events from the stream channel without writing
// them to SSE. This is called after the SSE client disconnects to prevent the
// channel from filling up and blocking the ACP agent process, which would
// cause "peer disconnected before response" crashes.
func drainStreamChannel(ch <-chan ai.StreamEvent, sessionID string) {
	for event := range ch {
		switch event.Type {
		case "done", "cancelled", "error":
			// Terminal event — the AI goroutine is finished, channel will be closed.
			slog.Debug("sse drain: terminal event", "type", event.Type, "session_id", sessionID)
			return
		}
	}
	slog.Debug("sse drain: channel closed", "session_id", sessionID)
}

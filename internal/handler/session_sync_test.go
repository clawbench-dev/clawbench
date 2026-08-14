package handler

import (
	"testing"

	"clawbench/internal/ai"
	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupLoadSessionReplay_CapturesMessageID(t *testing.T) {
	client := ai.NewClawBenchACPClient()
	u1 := "uuid-user-1"
	u2 := "uuid-user-2"
	a1 := "uuid-assistant-1"
	client.SetLoadSessionBufForTest([]acp.SessionNotification{
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: &u1, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hi"}}}}},
		{SessionId: "s", Update: acp.SessionUpdate{AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			MessageId: &a1, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "hello"}}}}},
		{SessionId: "s", Update: acp.SessionUpdate{UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
			MessageId: &u2, Content: acp.ContentBlock{Text: &acp.ContentBlockText{Text: "again"}}}}},
	})

	msgs := groupLoadSessionReplay(client)
	require.Len(t, msgs, 3)
	assert.Equal(t, "user", msgs[0].role)
	assert.Equal(t, u1, msgs[0].extMsgID)
	assert.Equal(t, "assistant", msgs[1].role)
	assert.Equal(t, a1, msgs[1].extMsgID)
	assert.Equal(t, "user", msgs[2].role)
	assert.Equal(t, u2, msgs[2].extMsgID)
}

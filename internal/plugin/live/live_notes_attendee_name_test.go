package live

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// meetingAttendeeName is what the minutes call the user. It must prefer 参会称呼
// (the meeting's own name) and only fall back to the account nickname when the
// install never configured one — the two are deliberately separate identities.
func TestMeetingAttendeeName_PrefersMeetingName(t *testing.T) {
	origCfg, origNick := model.ConfigInstance, model.UserNickname
	t.Cleanup(func() { model.ConfigInstance, model.UserNickname = origCfg, origNick })

	model.UserNickname = "account-nick"

	model.ConfigInstance.Meeting.AttendeeName = "参会的人"
	assert.Equal(t, "参会的人", meetingAttendeeName())

	// Whitespace-only sanitizes to empty, which counts as "not configured".
	model.ConfigInstance.Meeting.AttendeeName = "   "
	assert.Equal(t, "account-nick", meetingAttendeeName())

	model.ConfigInstance.Meeting.AttendeeName = ""
	assert.Equal(t, "account-nick", meetingAttendeeName())

	// Control characters are collapsed rather than passed through into a unit
	// of speech that gets synthesized.
	model.ConfigInstance.Meeting.AttendeeName = "  张三 \t 李四  "
	assert.Equal(t, "张三 李四", meetingAttendeeName())
}

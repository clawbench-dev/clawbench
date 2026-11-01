package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

/**
 * The title a meeting gets before anyone knows what it was about.
 *
 * The fallback used to be the PROJECT's name, so every meeting in a project
 * was called the same thing ("clawbench") and the list said nothing about any
 * of them. It is a date now, and these tests pin the two halves of that: the
 * format itself, and the "is this still a placeholder?" question the minutes
 * job asks before letting the summary model rename the meeting.
 */

func TestPlaceholderMeetingTitle_IsTheStartTime(t *testing.T) {
	at := time.Date(2026, 11, 1, 17, 35, 0, 0, time.Local)

	title := PlaceholderMeetingTitle(at)

	assert.Equal(t, "11-01 17:35", title)
	assert.True(t, IsPlaceholderMeetingTitle(title), "the fallback must recognize itself")
}

func TestIsPlaceholderMeetingTitle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		title string
		want  bool
	}{
		{"date and time", "11-01 17:35", true},
		{"surrounding space", "  11-01 17:35  ", true},
		{"a name the user typed", "发版评审", false},
		{"a name with a date in it", "11 月 1 日复盘", false},
		{"out of range", "13-45 17:35", false},
		{"seconds included", "11-01 17:35:00", false},
		{"empty", "", false},
		{"the old project-name fallback", "clawbench", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPlaceholderMeetingTitle(tc.title))
		})
	}
}

func TestLiveNotesMinutesQuestion(t *testing.T) {
	// A placeholder is not a name: passing it as "会议标题：" invites a meeting
	// about timestamps.
	assert.Equal(t, "请根据以上内容生成会议纪要。", liveNotesMinutesQuestion(""))
	assert.Equal(t, "请根据以上内容生成会议纪要。", liveNotesMinutesQuestion("11-01 17:35"))

	named := liveNotesMinutesQuestion("发版评审")
	assert.True(t, strings.HasPrefix(named, "会议标题：发版评审\n"), "got %q", named)
}

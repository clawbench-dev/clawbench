package handler

import (
	"context"
	"database/sql"
	"io"
	"testing"

	"clawbench/internal/liveshare"
	"clawbench/internal/model"
	pluglive "clawbench/internal/plugin/live"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// 这三个辅助随会议纪要搬进了 internal/plugin/live，但 handler 里仍有不依赖
// 会议纪要接口、只需要"重置房间表 / 规范化项目路径 / 弄坏写库"的用例。

// resetLiveRegistry installs a fresh registry for a test and restores the
// process-wide one afterwards.
func resetLiveRegistry(t *testing.T) {
	t.Helper()
	prev := pluglive.LiveRegistry
	pluglive.LiveRegistry = liveshare.NewRegistry()
	t.Cleanup(func() { pluglive.LiveRegistry = prev })
}

// projPath normalizes a test project path the SAME way the server does.
func projPath(p string) string {
	return model.NormalizeProjectPath(p)
}

// breakWrites replaces the write handle with a closed DB, leaving reads working.
func breakWrites(t *testing.T, project string) {
	t.Helper()
	broken, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, broken.Close())
	t.Cleanup(store.SetDBForTest(broken, store.UnsafeReadDBForTest()))

	_, ok, err := store.ProjectIDByPath(project)
	require.NoError(t, err)
	require.True(t, ok, "the project row must exist before writes are broken")
}

// loudPCM builds n frames (100ms each) of a square wave at 0.25 full scale.
func loudPCM(frames int) []byte {
	out := make([]byte, 0, frames*3200)
	for range frames {
		frame := make([]byte, 3200)
		for j := 0; j < len(frame); j += 2 {
			v := int16(8192)
			frame[j] = byte(uint16(v))
			frame[j+1] = byte(uint16(v) >> 8)
		}
		out = append(out, frame...)
	}
	return out
}

// stubSTT is a canned STT provider for tests.
type stubSTT struct {
	text  string
	err   error
	calls int
}

func (s *stubSTT) Transcribe(context.Context, io.Reader, string) (string, error) {
	s.calls++
	return s.text, s.err
}

// swapClosedReadDB keeps the write handle healthy but points reads at a closed
// DB, so read-path failures can be exercised.
func swapClosedReadDB(t *testing.T, healthyWrite *sql.DB) func() {
	t.Helper()
	closedDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, closedDB.Close())
	return store.SetDBForTest(healthyWrite, closedDB)
}

// swapClosedWriteDB keeps reads healthy but points writes at a closed DB.
func swapClosedWriteDB(t *testing.T, healthyRead *sql.DB) func() {
	t.Helper()
	origWrite := store.UnsafeDBForTest()
	closedDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, closedDB.Close())
	cleanup := store.SetDBForTest(closedDB, healthyRead)
	return func() {
		cleanup()
		assert.Same(t, origWrite, store.UnsafeDBForTest(), "write handle must be restored")
	}
}

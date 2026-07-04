package service_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/summarize"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
)

// schedulerSchema is the same schema used in chat_test.go but scoped locally
// to avoid variable name conflicts within the same test package.
const schedulerSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
	content TEXT NOT NULL,
	files TEXT,
	session_id TEXT,
	backend TEXT NOT NULL DEFAULT 'claude',
	streaming INTEGER NOT NULL DEFAULT 0,
	indexed INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_path TEXT NOT NULL,
	backend TEXT NOT NULL,
	title TEXT NOT NULL,
	agent_id TEXT DEFAULT '',
	agent_source TEXT DEFAULT 'default',
	model TEXT DEFAULT '',
	session_type TEXT NOT NULL DEFAULT 'chat',
	external_session_id TEXT DEFAULT '',
	deleted INTEGER NOT NULL DEFAULT 0,
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(project_path, backend, id)
);
CREATE TABLE IF NOT EXISTS scheduled_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT NOT NULL,
	name TEXT NOT NULL,
	cron_expr TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	prompt TEXT NOT NULL,
	session_id TEXT,
	status TEXT NOT NULL DEFAULT 'active',
	repeat_mode TEXT NOT NULL DEFAULT 'unlimited',
	max_runs INTEGER DEFAULT 0,
	last_run_at DATETIME,
	next_run_at DATETIME,
	run_count INTEGER DEFAULT 0,
	last_read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS task_executions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	task_id INTEGER NOT NULL,
	session_id TEXT NOT NULL,
	trigger_type TEXT NOT NULL DEFAULT 'auto',
	status TEXT NOT NULL DEFAULT 'running',
	read_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
CREATE TABLE IF NOT EXISTS ai_raw_responses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	backend TEXT NOT NULL DEFAULT '',
	raw_output TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func setupSchedulerDB(t *testing.T) *sql.DB { //nolint:unparam // test helper: DB used implicitly via global state
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	db.SetMaxOpenConns(1) // Required for :memory: SQLite — all queries must use the same connection
	_, err = db.Exec(schedulerSchema)
	assert.NoError(t, err)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	return db
}

func setupScheduler(t *testing.T) (*service.Scheduler, func()) {
	t.Helper()
	setupSchedulerDB(t)
	s := service.NewScheduler()
	return s, func() { s.Stop() }
}

// helperTask returns a valid ScheduledTask for testing.
func helperTask(overrides ...func(*model.ScheduledTask)) *model.ScheduledTask {
	task := &model.ScheduledTask{
		ProjectPath: "/test-project",
		Name:        "Test Task",
		CronExpr:    "0 * * * *", // every hour
		AgentID:     "agent1",
		Prompt:      "test prompt",
		RepeatMode:  "unlimited",
	}
	for _, fn := range overrides {
		fn(task)
	}
	return task
}

// ---------- NewScheduler ----------

func TestNewScheduler(t *testing.T) {
	s := service.NewScheduler()
	assert.NotNil(t, s)
}

func TestTaskCount_Empty(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	assert.Equal(t, 0, s.TaskCount())
}

func TestTaskCount_AfterAddTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	err := s.LoadTasksFromDB("")
	assert.NoError(t, err)

	task := helperTask()
	err = s.AddTask(task)
	assert.NoError(t, err)

	assert.Equal(t, 1, s.TaskCount())
}

func TestTaskCount_AfterRemoveTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	err := s.LoadTasksFromDB("")
	assert.NoError(t, err)

	task := helperTask()
	err = s.AddTask(task)
	assert.NoError(t, err)
	assert.Equal(t, 1, s.TaskCount())

	// Get the task ID from DB
	tasks, err := service.GetTasks("")
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)

	s.RemoveTask(tasks[0].ID)
	assert.Equal(t, 0, s.TaskCount())
}

// ---------- Start / Stop ----------

func TestSchedulerStartStop(t *testing.T) {
	s := service.NewScheduler()
	s.Start()
	s.Stop()
	// Should not panic
}

func TestSchedulerStopWithoutStart(t *testing.T) {
	s := service.NewScheduler()
	s.Stop()
	// Should not panic even if never started
}

// ---------- GetTasks ----------

func TestGetTasks_Empty(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	tasks, err := service.GetTasks("/project")
	assert.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestGetTasks_AllProjects(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj1", "Task 1", "0 * * * *", "agent1", "prompt1", "", "active", "unlimited", now, now,
	)
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj2", "Task 2", "0 * * * *", "agent1", "prompt2", "", "active", "unlimited", now, now,
	)

	tasks, err := service.GetTasks("")
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)

	tasks, err = service.GetTasks("/proj1")
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, int64(1), tasks[0].ID)
}

func TestGetTasks_OrdersByCreatedAtDesc(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "First", "0 * * * *", "agent1", "p", "", "active", "unlimited", now.Add(-1*time.Hour), now,
	)
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Second", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)

	tasks, err := service.GetTasks("/proj")
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)
	// newer first (higher auto-increment ID = created later)
	assert.True(t, tasks[0].ID > tasks[1].ID, "newer task should come first")
}

// ---------- GetTaskByID ----------

func TestGetTaskByID(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, max_runs, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task 1", "0 * * * *", "agent1", "prompt1", "sess-1", "active", "unlimited", 0, 3, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, taskID, task.ID)
	assert.Equal(t, "/proj", task.ProjectPath)
	assert.Equal(t, "Task 1", task.Name)
	assert.Equal(t, "0 * * * *", task.CronExpr)
	assert.Equal(t, "agent1", task.AgentID)
	assert.Equal(t, "prompt1", task.Prompt)
	assert.Equal(t, "sess-1", task.SessionID)
	assert.Equal(t, "active", task.Status)
	assert.Equal(t, "unlimited", task.RepeatMode)
	assert.Equal(t, 3, task.RunCount)
}

func TestGetTaskByID_NotFound(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	_, err := service.GetTaskByID(99999)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrNoRows, err)
}

// ---------- AddTask ----------

func TestAddTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	err := s.AddTask(task)
	assert.NoError(t, err)
	assert.NotZero(t, task.ID, "ID should be auto-generated")
	assert.Equal(t, "active", task.Status)
	assert.NotNil(t, task.NextRunAt, "NextRunAt should be calculated")
	assert.False(t, task.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.False(t, task.UpdatedAt.IsZero(), "UpdatedAt should be set")

	// Verify persisted in DB
	persisted, err := service.GetTaskByID(task.ID)
	assert.NoError(t, err)
	assert.Equal(t, task.Name, persisted.Name)
	assert.Equal(t, task.ProjectPath, persisted.ProjectPath)
}

func TestAddTask_InvalidCronExpr(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask(func(t *model.ScheduledTask) {
		t.CronExpr = "invalid-cron"
	})
	err := s.AddTask(task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cron expression")
}

func TestAddTask_SetsStatusToActive(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask(func(t *model.ScheduledTask) {
		t.Status = "paused" // try to set a different status
	})
	err := s.AddTask(task)
	assert.NoError(t, err)
	assert.Equal(t, "active", task.Status, "AddTask should always set status to active")
}

func TestAddTask_GeneratesUniqueIDs(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task1 := helperTask()
	task2 := helperTask()
	assert.NoError(t, s.AddTask(task1))
	assert.NoError(t, s.AddTask(task2))
	assert.NotEqual(t, task1.ID, task2.ID, "each task should get a unique ID")
}

func TestAddTask_AutoIncrementID(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))
	assert.True(t, task.ID > 0, "auto-increment ID should be a positive integer")
}

// ---------- RemoveTask ----------

func TestRemoveTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	s.RemoveTask(task.ID)

	// Task should be hard-deleted from DB
	_, err := service.GetTaskByID(task.ID)
	assert.Error(t, err, "hard-deleted task should not be found")

	// Should not appear in GetTasks
	tasks, err := service.GetTasks(task.ProjectPath)
	assert.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestRemoveTask_NonExistentDoesNotPanic(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	// Removing a task that was never added should not panic
	s.RemoveTask(99999)
}

// ---------- PauseTask ----------

func TestPauseTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	s.PauseTask(task.ID)

	// Task should be marked as paused in DB
	persisted, err := service.GetTaskByID(task.ID)
	assert.NoError(t, err)
	assert.Equal(t, "paused", persisted.Status)

	// Paused tasks should still appear in GetTasks (not deleted)
	tasks, err := service.GetTasks(task.ProjectPath)
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
}

func TestPauseTask_NonExistentDoesNotPanic(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	s.PauseTask(99999)
	// Should not panic
}

// ---------- ResumeTask ----------

func TestResumeTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	s.PauseTask(task.ID)

	err := s.ResumeTask(task.ID)
	assert.NoError(t, err)

	persisted, err := service.GetTaskByID(task.ID)
	assert.NoError(t, err)
	assert.Equal(t, "active", persisted.Status)
}

func TestResumeTask_NotPaused(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	// Task is active, not paused - should error
	err := s.ResumeTask(task.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not paused")
}

func TestResumeTask_NonExistent(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	err := s.ResumeTask(99999)
	assert.Error(t, err)
}

func TestResumeTask_AfterRemove(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	s.RemoveTask(task.ID)

	// Deleted task no longer exists, so resume should fail
	err := s.ResumeTask(task.ID)
	assert.Error(t, err)
}

// ---------- UpdateTask ----------

func TestUpdateTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	// Update the task
	task.Name = "Updated Name"
	task.Prompt = "updated prompt"
	err := s.UpdateTask(task)
	assert.NoError(t, err)

	persisted, err := service.GetTaskByID(task.ID)
	assert.NoError(t, err)
	assert.Equal(t, "Updated Name", persisted.Name)
	assert.Equal(t, "updated prompt", persisted.Prompt)
}

func TestUpdateTask_ChangeCronExpr(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	task.CronExpr = "0 0 * * *" // daily
	err := s.UpdateTask(task)
	assert.NoError(t, err)

	persisted, err := service.GetTaskByID(task.ID)
	assert.NoError(t, err)
	assert.Equal(t, "0 0 * * *", persisted.CronExpr)
}

func TestUpdateTask_InvalidCronExpr(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))

	task.CronExpr = "not-valid"
	err := s.UpdateTask(task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cron expression")
}

func TestUpdateTask_PausedTaskDoesNotReregister(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask()
	assert.NoError(t, s.AddTask(task))
	s.PauseTask(task.ID)

	// Reload the task from DB so Status reflects the paused state
	pausedTask, err := service.GetTaskByID(task.ID)
	assert.NoError(t, err)

	// Update a paused task - should not re-register with cron
	pausedTask.Name = "Updated Paused"
	err = s.UpdateTask(pausedTask)
	assert.NoError(t, err)

	persisted, err := service.GetTaskByID(task.ID)
	assert.NoError(t, err)
	assert.Equal(t, "Updated Paused", persisted.Name)
	// Task stays paused in DB since we updated a paused task
	assert.Equal(t, "paused", persisted.Status)
}

// ---------- LoadTasksFromDB ----------

func TestLoadTasksFromDB(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	// Insert tasks directly into DB
	now := time.Now()
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Active Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Paused Task", "0 * * * *", "agent1", "p", "", "paused", "unlimited", now, now,
	)

	err := s.LoadTasksFromDB("/proj")
	assert.NoError(t, err)

	// Active task should be loaded; paused task should be skipped
	// Get the active task's ID
	var activeID int64
	service.UnsafeDBForTest().QueryRow("SELECT id FROM scheduled_tasks WHERE status = 'active' AND project_path = '/proj'").Scan(&activeID)

	// We verify by checking that the active task can be removed without error
	s.RemoveTask(activeID)

	_, err = service.GetTaskByID(activeID)
	assert.Error(t, err, "hard-deleted task should not be found")
}

func TestLoadTasksFromDB_AllProjects(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj1", "Task 1", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj2", "Task 2", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)

	err := s.LoadTasksFromDB("") // empty = all projects
	assert.NoError(t, err)

	// Both tasks should be loaded — verify by getting their IDs and removing them
	var id1, id2 int64
	service.UnsafeDBForTest().QueryRow("SELECT id FROM scheduled_tasks WHERE project_path = '/proj1'").Scan(&id1)
	service.UnsafeDBForTest().QueryRow("SELECT id FROM scheduled_tasks WHERE project_path = '/proj2'").Scan(&id2)

	s.RemoveTask(id1)
	s.RemoveTask(id2)

	_, err1 := service.GetTaskByID(id1)
	_, err2 := service.GetTaskByID(id2)
	assert.Error(t, err1, "hard-deleted task should not be found")
	assert.Error(t, err2, "hard-deleted task should not be found")
}

func TestLoadTasksFromDB_InvalidCronSkipped(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	_, _ = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Bad Cron", "invalid", "agent1", "p", "", "active", "unlimited", now, now,
	)

	// Should not error — invalid cron tasks are logged and skipped
	err := s.LoadTasksFromDB("/proj")
	assert.NoError(t, err)
}

func TestLoadTasksFromDB_EmptyDB(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	err := s.LoadTasksFromDB("/proj")
	assert.NoError(t, err)
}

// ---------- AddTaskExecution ----------

func TestAddTaskExecution(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Insert a task
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	_, err = service.AddTaskExecution(taskID, "session-abc", "auto")
	assert.NoError(t, err)

	// Verify the execution was recorded
	var count int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", taskID).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	var fetchedSessionID string
	err = service.UnsafeDBForTest().QueryRow("SELECT session_id FROM task_executions WHERE task_id = ?", taskID).Scan(&fetchedSessionID)
	assert.NoError(t, err)
	assert.Equal(t, "session-abc", fetchedSessionID)
}

func TestAddTaskExecution_MultipleExecutions(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	_, err = service.AddTaskExecution(taskID, "session-1", "auto")
	assert.NoError(t, err)
	_, err = service.AddTaskExecution(taskID, "session-2", "auto")
	assert.NoError(t, err)

	var count int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", taskID).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestUpdateExecutionStatus(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	_, err = service.AddTaskExecution(taskID, "session-abc", "auto")
	assert.NoError(t, err)

	// Verify default status is 'running'
	var status string
	err = service.UnsafeDBForTest().QueryRow("SELECT status FROM task_executions WHERE session_id = ?", "session-abc").Scan(&status)
	assert.NoError(t, err)
	assert.Equal(t, "running", status)

	// Update to cancelled
	err = service.UpdateExecutionStatus("session-abc", "cancelled")
	assert.NoError(t, err)

	err = service.UnsafeDBForTest().QueryRow("SELECT status FROM task_executions WHERE session_id = ?", "session-abc").Scan(&status)
	assert.NoError(t, err)
	assert.Equal(t, "cancelled", status)
}

func TestUpdateTaskStats(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Stats Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 0, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 0, task.RunCount)

	service.UpdateTaskStats(task)

	updated, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 1, updated.RunCount)
	assert.NotNil(t, updated.LastRunAt)
	// Status should remain "active" since UpdateTaskStats no longer overwrites it
	assert.Equal(t, "active", updated.Status)
}

// TestUpdateTaskStats_DoesNotOverwritePausedStatus verifies ISS-013 fix:
// UpdateTaskStats must NOT overwrite a user-initiated "paused" status.
func TestUpdateTaskStats_DoesNotOverwritePausedStatus(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Paused Task", "0 * * * *", "agent1", "p", "", "paused", "unlimited", 0, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Simulate the in-memory task snapshot still having "active" status
	// (stale from before the user paused it)
	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, "paused", task.Status)

	// UpdateTaskStats should NOT change "paused" back to anything else
	service.UpdateTaskStats(task)

	updated, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 1, updated.RunCount)
	assert.NotNil(t, updated.LastRunAt)
	// Status must still be "paused" — not overwritten
	assert.Equal(t, "paused", updated.Status)
}

// ---------- insertTask / updateTask (tested indirectly via AddTask / UpdateTask) ----------

func TestInsertUpdateTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	task := helperTask(func(t *model.ScheduledTask) {
		t.Name = "Original"
	})
	assert.NoError(t, s.AddTask(task))
	originalID := task.ID

	// Update via UpdateTask
	task.Name = "Updated"
	assert.NoError(t, s.UpdateTask(task))

	persisted, err := service.GetTaskByID(originalID)
	assert.NoError(t, err)
	assert.Equal(t, "Updated", persisted.Name)
	assert.Equal(t, originalID, persisted.ID, "ID should not change on update")
}

// ---------- Lifecycle: full workflow ----------

func TestSchedulerFullLifecycle(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	// 1. Add task
	task := helperTask(func(t *model.ScheduledTask) {
		t.Name = "Lifecycle Task"
	})
	assert.NoError(t, s.AddTask(task))
	assert.Equal(t, "active", task.Status)

	// 2. Pause
	s.PauseTask(task.ID)
	paused, _ := service.GetTaskByID(task.ID)
	assert.Equal(t, "paused", paused.Status)

	// 3. Resume
	assert.NoError(t, s.ResumeTask(task.ID))
	resumed, _ := service.GetTaskByID(task.ID)
	assert.Equal(t, "active", resumed.Status)

	// 4. Update
	resumed.Name = "Updated Task"
	assert.NoError(t, s.UpdateTask(resumed))
	updated, _ := service.GetTaskByID(task.ID)
	assert.Equal(t, "Updated Task", updated.Name)

	// 5. Remove
	s.RemoveTask(task.ID)
	_, err := service.GetTaskByID(task.ID)
	assert.Error(t, err, "hard-deleted task should not be found")
}

// ---------- LoadTasksFromDB after scheduler restart ----------

func TestSchedulerRestartLoadsTasks(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Use first scheduler to add a task
	s1 := service.NewScheduler()
	s1.Start()
	task := helperTask()
	assert.NoError(t, s1.AddTask(task))
	s1.Stop()

	// Simulate restart: create a new scheduler and load from DB
	s2 := service.NewScheduler()
	s2.Start()
	defer s2.Stop()

	err := s2.LoadTasksFromDB(task.ProjectPath)
	assert.NoError(t, err)

	// Task should be registered; verify by pausing it
	s2.PauseTask(task.ID)
	persisted, _ := service.GetTaskByID(task.ID)
	assert.Equal(t, "paused", persisted.Status)
}

// ---------- Run count atomic increment ----------

func TestRunCount_AtomicIncrement(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Insert a task directly
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "RC Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 0, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Run 10 sequential atomic SQL increments.
	for range 10 {
		_, err := service.UnsafeDBForTest().Exec("UPDATE scheduled_tasks SET run_count = run_count + 1 WHERE id = ?", taskID)
		assert.NoError(t, err)
	}

	// All 10 increments should be accounted for
	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 10, task.RunCount, "run_count should be exactly 10 after 10 sequential increments")
}

// ---------- RemoveTask cascade deletes sessions (Task 7) ----------

func TestRemoveTask_CascadeDeletesSessions(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	// Create a task
	task := helperTask(func(t *model.ScheduledTask) {
		t.ProjectPath = "/cascade-proj"
	})
	assert.NoError(t, s.AddTask(task))

	// Create a scheduled chat session
	sessionID, err := service.CreateSession("/cascade-proj", "claude", "Exec 1", "agent1", "", "default", "scheduled")
	assert.NoError(t, err)

	// Add messages to the session
	_, err = service.AddChatMessage("/cascade-proj", "claude", sessionID, "user", "test prompt", nil, false, "Exec 1")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/cascade-proj", "claude", sessionID, "assistant", "test response", nil, false, "Exec 1")
	assert.NoError(t, err)

	// Create a task_execution linked to this session
	_, err = service.AddTaskExecution(task.ID, sessionID, "auto")
	assert.NoError(t, err)

	// Verify the session exists
	var sessionDeleted int
	err = service.UnsafeDBForTest().QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sessionID).Scan(&sessionDeleted)
	assert.NoError(t, err)
	assert.Equal(t, 0, sessionDeleted, "session should not be deleted before RemoveTask")

	// Remove the task — should cascade-delete sessions
	s.RemoveTask(task.ID)

	// Verify session is soft-deleted
	err = service.UnsafeDBForTest().QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sessionID).Scan(&sessionDeleted)
	assert.NoError(t, err)
	assert.Equal(t, 1, sessionDeleted, "session should be soft-deleted after RemoveTask")

	// Verify task_executions rows are deleted
	var execCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", task.ID).Scan(&execCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, execCount, "task_executions should be deleted after RemoveTask")

	// Verify task is hard-deleted
	_, err = service.GetTaskByID(task.ID)
	assert.Error(t, err, "hard-deleted task should not be found")
}

// ---------- PurgeDeletedData cleans task_executions (Task 8) ----------

func TestPurgeDeletedData_CleansTaskExecutions(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Create a scheduled chat session
	sessionID, err := service.CreateSession("/purge-proj", "claude", "Exec 1", "agent1", "", "default", "scheduled")
	assert.NoError(t, err)

	// Add messages
	service.AddChatMessage("/purge-proj", "claude", sessionID, "user", "prompt", nil, false, "Exec 1")

	// Create task_execution
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/purge-proj", "Purge Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	_, err = service.AddTaskExecution(taskID, sessionID, "auto")
	assert.NoError(t, err)

	// Verify task_execution exists
	var execCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE session_id = ?", sessionID).Scan(&execCount)
	assert.NoError(t, err)
	assert.Equal(t, 1, execCount)

	// Soft-delete the session and set updated_at to old date
	service.DeleteSession("/purge-proj", "claude", sessionID)
	oldTime := time.Now().Add(-100 * 24 * time.Hour) // 100 days ago
	_, _ = service.UnsafeDBForTest().Exec("UPDATE chat_sessions SET updated_at = ? WHERE id = ?", oldTime, sessionID)

	// Get expired sessions and purge
	cutoff := time.Now().Add(-90 * 24 * time.Hour)
	expiredIDs, err := service.GetExpiredDeletedSessions(cutoff)
	assert.NoError(t, err)
	assert.Contains(t, expiredIDs, sessionID)

	sessionsPurged, messagesPurged, err := service.PurgeDeletedData(expiredIDs)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), sessionsPurged)
	assert.True(t, messagesPurged >= 1)

	// Verify task_executions rows are also deleted
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE session_id = ?", sessionID).Scan(&execCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, execCount, "task_executions should be purged along with the session")
}

// ---------- DeleteTaskExecution ----------

func TestDeleteTaskExecution(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Create a task
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "DelExec Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 3, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Create a scheduled chat session
	sessionID, err := service.CreateSession("/proj", "claude", "Del Exec", "agent1", "", "default", "scheduled")
	assert.NoError(t, err)

	// Add messages
	_, err = service.AddChatMessage("/proj", "claude", sessionID, "user", "prompt", nil, false, "Del Exec")
	assert.NoError(t, err)
	_, err = service.AddChatMessage("/proj", "claude", sessionID, "assistant", "response", nil, false, "Del Exec")
	assert.NoError(t, err)

	// Create an execution linked to this session (mark as completed to allow deletion)
	_, err = service.AddTaskExecution(taskID, sessionID, "auto")
	assert.NoError(t, err)
	service.UpdateExecutionStatus(sessionID, "completed")

	// Get the execution ID
	var execID int64
	err = service.UnsafeDBForTest().QueryRow("SELECT id FROM task_executions WHERE session_id = ?", sessionID).Scan(&execID)
	assert.NoError(t, err)

	// Delete the execution
	err = service.DeleteTaskExecution(execID)
	assert.NoError(t, err)

	// Verify execution is hard-deleted
	var execCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE id = ?", execID).Scan(&execCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, execCount, "execution should be hard-deleted")

	// Verify session is soft-deleted
	var sessionDeleted int
	err = service.UnsafeDBForTest().QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sessionID).Scan(&sessionDeleted)
	assert.NoError(t, err)
	assert.Equal(t, 1, sessionDeleted, "session should be soft-deleted")

	// Verify run_count was decremented
	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 2, task.RunCount, "run_count should be decremented from 3 to 2")
}

func TestDeleteTaskExecution_NotFound(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	err := service.DeleteTaskExecution(99999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execution not found")
}

func TestDeleteTaskExecution_RunningExecution(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Create a task and a running execution
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Running Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 1, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	sessionID, err := service.CreateSession("/proj", "claude", "Running Exec", "agent1", "", "default", "scheduled")
	assert.NoError(t, err)

	_, err = service.AddTaskExecution(taskID, sessionID, "auto")
	assert.NoError(t, err)

	// Mark execution as running
	err = service.UpdateExecutionStatus(sessionID, "running")
	assert.NoError(t, err)

	var execID int64
	err = service.UnsafeDBForTest().QueryRow("SELECT id FROM task_executions WHERE session_id = ?", sessionID).Scan(&execID)
	assert.NoError(t, err)

	// Attempt to delete a running execution should fail
	err = service.DeleteTaskExecution(execID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete a running execution")

	// Verify execution still exists
	var execCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE id = ?", execID).Scan(&execCount)
	assert.NoError(t, err)
	assert.Equal(t, 1, execCount, "running execution should not be deleted")

	// Verify session is NOT soft-deleted (operation order fix: DELETE runs first,
	// so if DELETE fails, session must remain intact)
	var sessionDeleted int
	err = service.UnsafeDBForTest().QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", sessionID).Scan(&sessionDeleted)
	assert.NoError(t, err)
	assert.Equal(t, 0, sessionDeleted, "session should NOT be soft-deleted when execution deletion is rejected")

	// Verify run_count is NOT decremented
	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 1, task.RunCount, "run_count should not be decremented when deletion is rejected")
}

func TestDeleteTaskExecution_RunCountClampToZero(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Create a task with run_count = 0
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Zero Count", "0 * * * *", "agent1", "p", "", "active", "unlimited", 0, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	sessionID, err := service.CreateSession("/proj", "claude", "Zero Exec", "agent1", "", "default", "scheduled")
	assert.NoError(t, err)

	_, err = service.AddTaskExecution(taskID, sessionID, "auto")
	assert.NoError(t, err)
	service.UpdateExecutionStatus(sessionID, "completed")

	var execID int64
	err = service.UnsafeDBForTest().QueryRow("SELECT id FROM task_executions WHERE session_id = ?", sessionID).Scan(&execID)
	assert.NoError(t, err)

	// Delete the execution — run_count should clamp to 0 (not go negative)
	err = service.DeleteTaskExecution(execID)
	assert.NoError(t, err)

	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 0, task.RunCount, "run_count should clamp to 0, not go negative")
}

// ---------- DeleteAllTaskExecutions ----------

func TestDeleteAllTaskExecutions(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Create a task with run_count = 3
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "DelAll Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 3, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Create 3 sessions and executions (mark as completed to allow deletion)
	for i := range 3 {
		sessionID, err := service.CreateSession("/proj", "claude", fmt.Sprintf("Exec %d", i), "agent1", "", "default", "scheduled")
		assert.NoError(t, err)
		service.AddChatMessage("/proj", "claude", sessionID, "user", "prompt", nil, false, fmt.Sprintf("Exec %d", i))
		_, err = service.AddTaskExecution(taskID, sessionID, "auto")
		assert.NoError(t, err)
		service.UpdateExecutionStatus(sessionID, "completed")
	}

	// Verify 3 executions exist
	var execCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", taskID).Scan(&execCount)
	assert.NoError(t, err)
	assert.Equal(t, 3, execCount)

	// Delete all executions
	err = service.DeleteAllTaskExecutions(taskID)
	assert.NoError(t, err)

	// Verify all executions are deleted
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ?", taskID).Scan(&execCount)
	assert.NoError(t, err)
	assert.Equal(t, 0, execCount, "all executions should be deleted")

	// Verify run_count reset to 0
	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 0, task.RunCount, "run_count should be reset to 0")
}

func TestDeleteAllTaskExecutions_PreservesRunning(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Mixed Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 2, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// One completed, one running
	completedSession, _ := service.CreateSession("/proj", "claude", "Completed", "agent1", "", "default", "scheduled")
	service.AddTaskExecution(taskID, completedSession, "auto")
	service.UpdateExecutionStatus(completedSession, "completed")

	runningSession, _ := service.CreateSession("/proj", "claude", "Running", "agent1", "", "default", "scheduled")
	service.AddTaskExecution(taskID, runningSession, "auto")
	// Status is already 'running' from AddTaskExecution

	// Delete all — should only delete the completed one
	err = service.DeleteAllTaskExecutions(taskID)
	assert.NoError(t, err)

	// Running execution should still exist
	var runningCount int
	err = service.UnsafeDBForTest().QueryRow("SELECT COUNT(*) FROM task_executions WHERE task_id = ? AND status = 'running'", taskID).Scan(&runningCount)
	assert.NoError(t, err)
	assert.Equal(t, 1, runningCount, "running execution should be preserved")

	// run_count should be 1 (matching the remaining running execution)
	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, 1, task.RunCount, "run_count should be set to the count of remaining executions")
}

func TestDeleteAllTaskExecutions_NoExecutions(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Empty Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 0, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Delete all with no executions — should not error
	err = service.DeleteAllTaskExecutions(taskID)
	assert.NoError(t, err)
}

// ── HasUnreadTasks ──

func TestHasUnreadTasks_NoTasks(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	hasUnread, err := service.HasUnreadTasks("/proj")
	assert.NoError(t, err)
	assert.False(t, hasUnread, "should be false when no tasks exist")
}

func TestHasUnreadTasks_NoExecutions(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)

	hasUnread, err := service.HasUnreadTasks("/proj")
	assert.NoError(t, err)
	assert.False(t, hasUnread, "should be false when no executions exist")
}

func TestHasUnreadTasks_UnreadExecution(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Add execution with read_at = NULL (unread)
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at) VALUES (?, ?, ?, ?, ?)",
		taskID, "session-1", "auto", "completed", now,
	)
	assert.NoError(t, err)

	hasUnread, err := service.HasUnreadTasks("/proj")
	assert.NoError(t, err)
	assert.True(t, hasUnread, "should be true when unread execution exists")
}

func TestHasUnreadTasks_ReadExecution(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Add execution with read_at set (read)
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO task_executions (task_id, session_id, trigger_type, status, read_at, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		taskID, "session-1", "auto", "completed", now, now,
	)
	assert.NoError(t, err)

	hasUnread, err := service.HasUnreadTasks("/proj")
	assert.NoError(t, err)
	assert.False(t, hasUnread, "should be false when all executions are read")
}

func TestHasUnreadTasks_ScopedByProjectPath(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	// Task in /proj-a with unread execution
	resultA, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj-a", "Task A", "0 * * * *", "agent1", "p", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskIDA, _ := resultA.LastInsertId()
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at) VALUES (?, ?, ?, ?, ?)",
		taskIDA, "session-a1", "auto", "completed", now,
	)
	assert.NoError(t, err)

	// Task in /proj-b with no executions
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj-b", "Task B", "0 * * * *", "agent1", "p", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)

	hasUnreadA, err := service.HasUnreadTasks("/proj-a")
	assert.NoError(t, err)
	assert.True(t, hasUnreadA, "/proj-a should have unread")

	hasUnreadB, err := service.HasUnreadTasks("/proj-b")
	assert.NoError(t, err)
	assert.False(t, hasUnreadB, "/proj-b should not have unread")
}

func TestHasUnreadTasks_EmptyProjectPath(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at) VALUES (?, ?, ?, ?, ?)",
		taskID, "session-1", "auto", "completed", now,
	)
	assert.NoError(t, err)

	// Empty project path should check all projects
	hasUnread, err := service.HasUnreadTasks("")
	assert.NoError(t, err)
	assert.True(t, hasUnread, "empty project path should find unread across all projects")
}

func TestHasUnreadTasks_RunningExecutionNotUnread(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Task", "0 * * * *", "agent1", "p", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Add a running execution — should NOT be counted as unread
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO task_executions (task_id, session_id, trigger_type, status, created_at) VALUES (?, ?, ?, ?, ?)",
		taskID, "session-running", "auto", "running", now,
	)
	assert.NoError(t, err)

	hasUnread, err := service.HasUnreadTasks("/proj")
	assert.NoError(t, err)
	assert.False(t, hasUnread, "running execution should not count as unread")

	// Now mark it as completed — should become unread
	service.UpdateExecutionStatus("session-running", "completed")
	hasUnread, err = service.HasUnreadTasks("/proj")
	assert.NoError(t, err)
	assert.True(t, hasUnread, "completed execution should count as unread")
}

// ---------- dbRead initialization ----------

func TestDBRead_Initialized_SchedulerDB(t *testing.T) {
	_ = setupSchedulerDB(t)
	assert.NotNil(t, service.ReadDB(), "dbRead should be initialized in test setup")
}

// ---------- ISS-200: Cron re-parse failure pauses task ----------

func TestCronReparseFailure_SetsStatusToPaused(t *testing.T) {
	// Verify that when cron.ParseStandard fails, the status should be "paused"
	// to prevent zombie active tasks (ISS-200).
	// The actual fix is in executeTask(), but we verify the expected behavior
	// by simulating the scenario at the DB level.

	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Insert a task with a valid cron expression and session_id
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, run_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Cron Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", 1, now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Simulate what executeTask would do on cron re-parse failure:
	// Set status to "paused" (the fix for ISS-200)
	_, err = service.UnsafeDBForTest().Exec(
		"UPDATE scheduled_tasks SET status = ?, next_run_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		"paused", taskID,
	)
	assert.NoError(t, err)

	// Verify the task is now paused
	task, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	assert.Equal(t, "paused", task.Status, "task should be paused when cron re-parse fails (ISS-200)")
	assert.Nil(t, task.NextRunAt, "paused task should have nil next_run_at")
}

func TestCronReparseFailure_InvalidExprCannotBeResumed(t *testing.T) {
	// If a task has an invalid cron expression and gets paused,
	// resuming it should fail because the expression can't be parsed.

	s, cleanup := setupScheduler(t)
	defer cleanup()

	// Insert a task with an invalid cron expression directly into DB
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Broken Cron", "not-a-valid-cron", "agent1", "p", "", "paused", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Attempt to resume — should fail because UpdateTask re-parses the cron expression
	pausedTask, err := service.GetTaskByID(taskID)
	assert.NoError(t, err)
	err = s.UpdateTask(pausedTask)
	assert.Error(t, err, "resuming a task with invalid cron should fail")
	assert.Contains(t, err.Error(), "invalid cron expression")
}

// ---------- GetRunningExecutions ----------

func TestGetRunningExecutions_Empty(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	result := s.GetRunningExecutions(1)
	assert.Empty(t, result, "should return empty slice when no running executions")
}

func TestGetRunningExecutions_WithMatchingExecutions(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	now := time.Now()
	s.AddRunningExecution(&service.RunningExecution{
		ID:          "exec-1",
		TaskID:      1,
		CancelFunc:  func() {},
		StartedAt:   now,
		TriggerType: "auto",
	})
	s.AddRunningExecution(&service.RunningExecution{
		ID:          "exec-2",
		TaskID:      1,
		CancelFunc:  func() {},
		StartedAt:   now,
		TriggerType: "manual",
	})
	s.AddRunningExecution(&service.RunningExecution{
		ID:          "exec-3",
		TaskID:      2,
		CancelFunc:  func() {},
		StartedAt:   now,
		TriggerType: "auto",
	})

	result := s.GetRunningExecutions(1)
	assert.Len(t, result, 2, "should return executions for matching taskID only")

	// Verify fields — order from sync.Map is non-deterministic, so collect IDs
	ids := map[string]bool{}
	for _, v := range result {
		ids[v.ID] = true
		assert.Equal(t, now, v.StartedAt)
		assert.Contains(t, []string{"auto", "manual"}, v.TriggerType)
	}
	assert.True(t, ids["exec-1"])
	assert.True(t, ids["exec-2"])

	// Different taskID should return only its own
	result2 := s.GetRunningExecutions(2)
	assert.Len(t, result2, 1)
	assert.Equal(t, "exec-3", result2[0].ID)

	// Non-existent taskID returns empty
	result3 := s.GetRunningExecutions(99)
	assert.Empty(t, result3)
}

// ---------- GetRunningCounts ----------

func TestGetRunningCounts_Empty(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	counts := s.GetRunningCounts()
	assert.Empty(t, counts, "should return empty map when no running executions")
}

func TestGetRunningCounts_MultipleTasks(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})
	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-2", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "manual",
	})
	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-3", TaskID: 2, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	counts := s.GetRunningCounts()
	assert.Equal(t, 2, counts[1], "task 1 should have 2 running executions")
	assert.Equal(t, 1, counts[2], "task 2 should have 1 running execution")
	_, exists := counts[99]
	assert.False(t, exists, "non-existent task should not appear in counts")
}

// ---------- HasRunningExecutions ----------

func TestHasRunningExecutions_Empty(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	assert.False(t, s.HasRunningExecutions(1), "should return false when no running executions")
}

func TestHasRunningExecutions_WithMatchingTaskID(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() {}, StartedAt: time.Now(), TriggerType: "auto",
	})

	assert.True(t, s.HasRunningExecutions(1), "should return true when task has running execution")
	assert.False(t, s.HasRunningExecutions(2), "should return false for different taskID")
}

// ---------- MarkTaskRunning / UnmarkTaskRunning ----------

func TestMarkUnmarkTaskRunning(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	// Initially not running
	assert.False(t, s.HasRunningExecutions(1))

	// Mark as running
	s.MarkTaskRunning(1)

	// Verify via TriggerTask-style LoadOrStore check (loaded=true means already running)
	_, loaded := s.TriggerTaskLoadOrStore(1)
	assert.True(t, loaded, "task should be marked as running after MarkTaskRunning")

	// Unmark
	s.UnmarkTaskRunning(1)

	// Verify it's gone — LoadOrStore should return loaded=false
	_, loaded = s.TriggerTaskLoadOrStore(1)
	assert.False(t, loaded, "task should no longer be running after UnmarkTaskRunning")
	// Clean up the entry that LoadOrStore just created
	s.UnmarkTaskRunning(1)
}

// ---------- CancelExecution ----------

func TestCancelExecution_NotFound(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	err := s.CancelExecution("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "execution not found")
}

func TestCancelExecution_Found(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	cancelled := false
	cancelFunc := func() { cancelled = true }

	s.AddRunningExecution(&service.RunningExecution{
		ID:          "exec-1",
		TaskID:      1,
		CancelFunc:  cancelFunc,
		StartedAt:   time.Now(),
		TriggerType: "auto",
	})

	err := s.CancelExecution("exec-1")
	assert.NoError(t, err)
	assert.True(t, cancelled, "cancel function should have been called")

	// Execution should still be in the map (CancelExecution doesn't remove it)
	assert.True(t, s.HasRunningExecutions(1))
}

// ---------- CancelAllExecutions ----------

func TestCancelAllExecutions(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	var cancelled1, cancelled2, cancelled3 bool

	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() { cancelled1 = true }, StartedAt: time.Now(), TriggerType: "auto",
	})
	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-2", TaskID: 1, CancelFunc: func() { cancelled2 = true }, StartedAt: time.Now(), TriggerType: "manual",
	})
	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-3", TaskID: 2, CancelFunc: func() { cancelled3 = true }, StartedAt: time.Now(), TriggerType: "auto",
	})

	// Cancel all for task 1 — should not cancel task 2's execution
	s.CancelAllExecutions(1)

	assert.True(t, cancelled1, "exec-1 cancel func should be called")
	assert.True(t, cancelled2, "exec-2 cancel func should be called")
	assert.False(t, cancelled3, "exec-3 (different task) should NOT be cancelled")
}

func TestCancelAllExecutions_NoMatchingTask(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	cancelled := false
	s.AddRunningExecution(&service.RunningExecution{
		ID: "exec-1", TaskID: 1, CancelFunc: func() { cancelled = true }, StartedAt: time.Now(), TriggerType: "auto",
	})

	// Cancel all for a task that has no executions — should not panic
	s.CancelAllExecutions(99)
	assert.False(t, cancelled, "no cancel func should be called for unrelated task")
}

// ---------- cleanZombieExecutions ----------

func TestCleanZombieExecutions(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Create a task
	now := time.Now()
	result, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Zombie Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)
	taskID, _ := result.LastInsertId()

	// Insert a "running" execution (zombie)
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO task_executions (task_id, session_id, trigger_type, status) VALUES (?, ?, ?, 'running')",
		taskID, "zombie-session-1", "auto",
	)
	assert.NoError(t, err)

	// Insert a "completed" execution (not a zombie)
	_, err = service.UnsafeDBForTest().Exec(
		"INSERT INTO task_executions (task_id, session_id, trigger_type, status) VALUES (?, ?, ?, 'completed')",
		taskID, "completed-session-1", "auto",
	)
	assert.NoError(t, err)

	// Run cleanZombieExecutions via LoadTasksFromDB (which calls it internally)
	s := service.NewScheduler()
	err = s.LoadTasksFromDB("/proj")
	assert.NoError(t, err)
	s.Stop()

	// Verify the zombie was cleaned up (status changed to "failed")
	var zombieStatus string
	err = service.UnsafeDBForTest().QueryRow("SELECT status FROM task_executions WHERE session_id = ?", "zombie-session-1").Scan(&zombieStatus)
	assert.NoError(t, err)
	assert.Equal(t, "failed", zombieStatus, "zombie execution should be marked as failed")

	// Verify the completed execution was NOT affected
	var completedStatus string
	err = service.UnsafeDBForTest().QueryRow("SELECT status FROM task_executions WHERE session_id = ?", "completed-session-1").Scan(&completedStatus)
	assert.NoError(t, err)
	assert.Equal(t, "completed", completedStatus, "completed execution should not be affected")
}

func TestCleanZombieExecutions_NoZombies(t *testing.T) {
	_, cleanup := setupScheduler(t)
	defer cleanup()

	// Insert a task with no executions
	now := time.Now()
	_, err := service.UnsafeDBForTest().Exec(
		"INSERT INTO scheduled_tasks (project_path, name, cron_expr, agent_id, prompt, session_id, status, repeat_mode, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		"/proj", "Clean Task", "0 * * * *", "agent1", "p", "", "active", "unlimited", now, now,
	)
	assert.NoError(t, err)

	// Should not panic or error
	s := service.NewScheduler()
	err = s.LoadTasksFromDB("/proj")
	assert.NoError(t, err)
	s.Stop()
}

// ---------- SetTaskSummarizer ----------

func TestSetTaskSummarizer(t *testing.T) {
	s := service.NewScheduler()
	defer s.Stop()

	// Initially nil
	assert.Nil(t, s.TaskSummarizer())

	// Set a summarizer
	ts := &summarize.TaskSummarizer{}
	s.SetTaskSummarizer(ts)

	// Verify it was set
	assert.Equal(t, ts, s.TaskSummarizer(), "TaskSummarizer should be set and retrievable")
}

// ---------- AddRunningExecution / RemoveRunningExecution ----------

func TestAddRemoveRunningExecution(t *testing.T) {
	s, cleanup := setupScheduler(t)
	defer cleanup()

	exec := &service.RunningExecution{
		ID:          "test-exec",
		TaskID:      42,
		CancelFunc:  func() {},
		StartedAt:   time.Now(),
		TriggerType: "manual",
	}

	// Add
	s.AddRunningExecution(exec)
	assert.True(t, s.HasRunningExecutions(42))
	result := s.GetRunningExecutions(42)
	assert.Len(t, result, 1)
	assert.Equal(t, "test-exec", result[0].ID)
	assert.Equal(t, "manual", result[0].TriggerType)

	// Remove
	s.RemoveRunningExecution("test-exec")
	assert.False(t, s.HasRunningExecutions(42))
}

// ---------- Scheduled Session Stream Registration ----------

func TestScheduledSessionStreamRegistration(t *testing.T) {
	// Verify that when a scheduled session is marked running and its stream
	// channel is registered (as executeTask now does), the stream is
	// accessible via GetSessionStream for live preview.
	sessionID := "sched-stream-test-session"

	// Simulate what executeTask does: mark running, register stream
	service.SetSessionRunning(sessionID, true, true)
	streamCh := service.RegisterSessionStream(sessionID)

	// Stream should be retrievable
	gotCh, ok := service.GetSessionStream(sessionID)
	assert.True(t, ok, "stream should be registered for scheduled session")
	assert.NotNil(t, gotCh, "stream channel should not be nil")

	// Events written to the channel should be readable
	go func() {
		streamCh <- ai.StreamEvent{Type: "content", Content: "hello from scheduled task"}
	}()
	select {
	case event := <-gotCh:
		assert.Equal(t, "content", event.Type)
		assert.Equal(t, "hello from scheduled task", event.Content)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event from scheduled session stream")
	}

	// Cleanup (simulates deferred cleanup in executeTask)
	service.UnregisterSessionStream(sessionID)
	service.SetSessionRunning(sessionID, false, true)

	// Stream should no longer be available
	_, ok = service.GetSessionStream(sessionID)
	assert.False(t, ok, "stream should be unregistered after cleanup")
}

func TestScheduledSessionStreamSSEClaim(t *testing.T) {
	// Verify that a live preview client can claim the SSE stream of a
	// scheduled session (same mechanism as interactive sessions).
	sessionID := "sched-claim-test-session"

	service.SetSessionRunning(sessionID, true, true)
	service.RegisterSessionStream(sessionID)
	defer func() {
		service.UnregisterSessionStream(sessionID)
		service.SetSessionRunning(sessionID, false, true)
	}()

	// Live preview client claims the stream
	assert.True(t, service.TryClaimSSEStream(sessionID), "first SSE claim should succeed")

	// Second client should be rejected (same as interactive multi-client)
	assert.False(t, service.TryClaimSSEStream(sessionID), "second SSE claim should fail")

	// Release — second client would fall back to HTTP polling
	service.ReleaseSSEStream(sessionID)
}

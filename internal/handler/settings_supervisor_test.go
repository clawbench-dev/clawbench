package handler

import (
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubSupervisorProbe points IsRunningUnderSupervisor' cgroup file and
// systemctl stub at test-controlled fixtures, and returns a restore func.
func stubSupervisorProbe(cgroupFixture string, mainPID func(unit string) string) func() {
	systemdCgroupPath = cgroupFixture
	systemctlShowFunc = mainPID
	return func() {
		systemdCgroupPath = "/proc/self/cgroup"
		systemctlShowFunc = systemctlShowMainPID
	}
}

func clearSupervisorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLAWBENCH_NO_SUPERVISOR", "")
	t.Setenv("container", "")
}

// ---------- currentSystemdUnit ----------

func TestCurrentSystemdUnit_ExtractsUnitFromCgroupPath(t *testing.T) {
	restore := stubSupervisorProbe("testdata/cgroup-tat-agent", nil) // "0::/system.slice/tat_agent.service"
	defer restore()

	assert.Equal(t, "tat_agent.service", currentSystemdUnit())
}

func TestCurrentSystemdUnit_NonServicePathReturnsEmpty(t *testing.T) {
	restore := stubSupervisorProbe("testdata/cgroup-container-only", nil) // 纯容器路径，无 .service
	defer restore()

	assert.Equal(t, "", currentSystemdUnit())
}

func TestCurrentSystemdUnit_UnreadableFileReturnsEmpty(t *testing.T) {
	restore := stubSupervisorProbe("testdata/does-not-exist", nil)
	defer restore()

	assert.Equal(t, "", currentSystemdUnit())
}

// ---------- unitMainPID ----------

func TestUnitMainPID_ReturnsNumericPID(t *testing.T) {
	restore := stubSupervisorProbe("", func(string) string { return "12345" })
	defer restore()

	assert.Equal(t, "12345", unitMainPID("clawbench.service"))
}

func TestUnitMainPID_ZeroMeansInactiveReturnsEmpty(t *testing.T) {
	restore := stubSupervisorProbe("", func(string) string { return "0" })
	defer restore()

	assert.Equal(t, "", unitMainPID("clawbench.service"))
}

func TestUnitMainPID_SpacesTrimmed(t *testing.T) {
	restore := stubSupervisorProbe("", func(string) string { return "  42  \n" })
	defer restore()

	assert.Equal(t, "42", unitMainPID("clawbench.service"))
}

// ---------- IsRunningUnderSupervisor (systemd MainPID branch) ----------

// 原版把继承自 systemd agent shell 的 INVOCATION_ID 当作"受托管"依据，
// 导致升级走"等 supervisor 拉起"分支而假死。修复后：身处某 unit 的 cgroup
// 但自己不是该 unit MainPID（例如从 tat_agent shell setsid 起的进程），
// 即使带 INVOCATION_ID 也必须判非托管。
func TestIsRunningUnderSupervisor_InAgentShellNotMainPIDIsNotSupervised(t *testing.T) {
	restore := stubSupervisorProbe("testdata/cgroup-tat-agent", func(unit string) string {
		if unit == "tat_agent.service" {
			return "8888" // 一个与当前测试进程不同的 MainPID
		}
		return ""
	})
	defer restore()
	clearSupervisorEnv(t)
	t.Setenv("INVOCATION_ID", "02e058d71812411ba98abd659f5fd10a")

	assert.False(t, IsRunningUnderSupervisor(),
		"in systemd unit cgroup but not the unit's MainPID => not supervised")
}

// 真托管：systemd 服务 ExecStart 的 MainPID 即本进程。
func TestIsRunningUnderSupervisor_IsMainPIDOfUnitIsSupervised(t *testing.T) {
	restore := stubSupervisorProbe("testdata/cgroup-clawbench-service",
		func(string) string { return strconv.Itoa(os.Getpid()) })
	defer restore()
	clearSupervisorEnv(t)
	t.Setenv("INVOCATION_ID", "some-id")

	assert.True(t, IsRunningUnderSupervisor(),
		"self is the unit's MainPID => supervised, upgrade waits for systemd restart")
}

// systemctl 不可用/unit inactive（MainPID 为空或 0）时，即使 cgroup 指向某 unit
// 也判非托管 —— 失败方向安全，走哨兵自启。
func TestIsRunningUnderSupervisor_UnitMainPIDUnknownIsNotSupervised(t *testing.T) {
	restore := stubSupervisorProbe("testdata/cgroup-tat-agent", func(string) string { return "" })
	defer restore()
	clearSupervisorEnv(t)
	t.Setenv("INVOCATION_ID", "some-id")

	assert.False(t, IsRunningUnderSupervisor(),
		"cannot confirm MainPID => treat as not supervised (fail-safe)")
}

// ---------- 容器/无 cgroup 分支（回归保护） ----------

func TestIsRunningUnderSupervisor_ContainerEnvStillSupervised(t *testing.T) {
	restore := stubSupervisorProbe("testdata/cgroup-container-only", func(string) string { return "" })
	defer restore()
	clearSupervisorEnv(t)
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("container", "docker")

	assert.True(t, IsRunningUnderSupervisor(), "container env => supervised")
}

func TestIsRunningUnderSupervisor_NoIndicatorsNoUnitReturnsFalse(t *testing.T) {
	restore := stubSupervisorProbe("testdata/cgroup-empty", func(string) string { return "" })
	defer restore()
	clearSupervisorEnv(t)
	t.Setenv("INVOCATION_ID", "")

	assert.False(t, IsRunningUnderSupervisor(), "no indicators and no unit => not supervised")
}

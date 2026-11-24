//go:build linux

package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// RunServiceCommand handles the "clawbench service" subcommand.
//
// Everything here is done by running systemctl and writing a unit file — there
// is no cgo and no D-Bus binding. The unit file is the only thing this command
// creates; systemd does the rest.
func RunServiceCommand(args []string) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintf(os.Stderr, "clawbench service: 只支持 Linux（当前系统是 %s）\n", runtime.GOOS)
		return 1
	}
	action := ""
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "install":
		return runServiceInstall(args[1:])
	case "uninstall":
		return runServiceUninstall()
	case "status":
		return runServiceStatus()
	case "", "help", "--help", "-h":
		printServiceUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "clawbench service: 未知动作 %q\n\n", action)
		printServiceUsage()
		return 2
	}
}

func printServiceUsage() {
	fmt.Println("用法: clawbench service <install|uninstall|status> [--binary PATH]")
	fmt.Println()
	fmt.Println("  install    把 ClawBench 注册成开机自动运行的服务")
	fmt.Println("  uninstall  卸载该服务（不会删数据目录）")
	fmt.Println("  status     查看服务当前状态")
	fmt.Println()
	fmt.Println("  --binary PATH  指定要注册的 clawbench 二进制路径。一般不需要：")
	fmt.Println("                 默认用 PATH 上那个固定入口（npm -g 装出来的那个），")
	fmt.Println("                 升级后会指向新版本，服务跟着用新版。")
	fmt.Println("                 只在你把程序放到 PATH 之外时才需要指定。")
}

// serviceUnitName is the systemd unit this command manages.
const serviceUnitName = "clawbench.service"

// serviceTarget describes where the unit file goes and how systemctl is called
// for the chosen install scope.
type serviceTarget struct {
	system bool   // true = 系统级（/etc/systemd/system, 需 sudo）
	dir    string // unit 文件所在目录
	user   string // 系统级的 User=；用户级为空
	home   string // HOME，必须显式写（见 buildUnitFile 注释）
}

// serviceOutcome is a decision plus the reason shown to the user when the
// decision is "refuse".
type serviceOutcome struct {
	ok     bool
	reason string
}

// decideServiceInstall reports whether this install can be registered as a
// systemd service, and why not when it cannot.
//
// The unit must pin a path that SURVIVES an upgrade. An npm install keeps the
// real binary under a version-numbered directory that is replaced wholesale on
// upgrade, so pinning that path kills the service at the first upgrade. The npm
// global prefix does have a stable entry point on PATH (a shim that resolves
// the current package and execs it), and that entry point is what gets pinned:
// it is a fixed location whose contents npm rewrites on upgrade, so the running
// service picks up the new version on its next restart.
//
// The extra process layer a shim adds does not matter here — systemd tracks the
// unit's MainPID, and the server's own "am I supervised?" check only asks
// whether this process is that MainPID.
func decideServiceInstall(exePath, stableEntry, dataDir string, systemdAvailable, isContainer, managed bool) (string, serviceOutcome) {
	if isContainer {
		return "", serviceOutcome{reason: "当前在容器里运行，服务的启停由容器来管（docker --restart / k8s），不需要也不应该再装一层 systemd。"}
	}
	if managed {
		return "", serviceOutcome{reason: "当前进程由 ClawBench 桌面客户端托管，服务只能由客户端启停。"}
	}
	if !systemdAvailable {
		return "", serviceOutcome{reason: "这台机器上没有可用的 systemctl，装不了 systemd 服务。"}
	}
	if exePath == "" {
		return "", serviceOutcome{reason: "解析不到当前二进制的路径，无法写服务文件。"}
	}
	// A stable entry point wins even when the binary itself lives in a
	// versioned directory: the entry point is the path that stays put.
	if stableEntry != "" && !isVersionedInstallPath(stableEntry) {
		return stableEntry, serviceOutcome{ok: true}
	}
	if isVersionedInstallPath(exePath) {
		return "", serviceOutcome{reason: fmt.Sprintf(
			"当前是从 npm 装的，二进制在带版本号的目录里，而 PATH 上找不到 clawbench 这个固定入口：\n  %s\n升级一次这个目录就变了，服务会起不来。\n请先用 `npm -g` 安装（或把 clawbench 放进 PATH），再执行本命令；也可以用 --binary 指定一个固定路径。", exePath)}
	}
	return exePath, serviceOutcome{ok: true}
}

// isVersionedInstallPath reports whether path looks like an npm package
// directory, where the real binary sits under a directory that carries the
// version and is replaced wholesale on upgrade.
func isVersionedInstallPath(path string) bool {
	norm := filepath.ToSlash(path)
	if strings.Contains(norm, "node_modules/") {
		return true
	}
	// npm 前缀布局：<prefix>/lib/node_modules/@scope/pkg/bin/clawbench
	if strings.Contains(norm, "/lib/node_modules/") {
		return true
	}
	return false
}

// buildUnitFile renders the systemd unit for this instalation.
//
// Environment=HOME is written explicitly in BOTH scopes. systemd does not guess
// HOME once User= is set for a system service, and a user unit's manager may
// not have it either; the AI CLIs ClawBench spawns read their config from $HOME
// and silently fall back to defaults without it, which looks like "the agent
// forgot its settings" rather than a missing variable.
//
// ExecStart runs the binary directly, never a shell wrapper and never the npm
// launcher: the server decides whether it is supervised by checking that it IS
// the unit's MainPID (internal/handler/settings.go IsRunningUnderSupervisor),
// and a wrapper breaks that check — the restart path would then start a
// sentinel and fight systemd for the data-directory lock.
func buildUnitFile(exe, dataDir, user, home string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=ClawBench Server\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n")
	// StartLimit* belong to [Unit], not [Service]: systemd silently IGNORES
	// them in [Service] ("Unknown key name ... ignoring"), which would leave the
	// crash-loop guard below unarmed while looking configured.
	// Crash-loop guard for Restart=always: a unit that dies immediately at
	// startup (bad config, missing data dir) would otherwise restart forever
	// and fill the journal; 5 tries in 10 minutes, then "failed" — diagnosable
	// from `systemctl status`.
	b.WriteString("StartLimitIntervalSec=600\n")
	b.WriteString("StartLimitBurst=5\n")
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	if user != "" {
		fmt.Fprintf(&b, "User=%s\n", user)
	}
	fmt.Fprintf(&b, "Environment=HOME=%s\n", home)
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", home)
	fmt.Fprintf(&b, "ExecStart=%s --data-dir %s\n", systemdQuote(exe), systemdQuote(dataDir))
	// SIGTERM is what the server already handles for a graceful shutdown; this
	// only documents it.
	b.WriteString("ExecReload=/bin/kill -TERM $MAINPID\n")
	// always, not on-failure: a self-upgrade ends with the process exiting
	// cleanly after swapping its own binary, and on-failure would leave the
	// freshly installed version sitting there unstarted. The cost is that a
	// `systemctl stop` is also "restarted" — except systemd does not do that:
	// a stop requested by an admin suppresses the restart rule.
	b.WriteString("Restart=always\n")
	b.WriteString("RestartSec=2\n")
	// The server drains live streams before closing the listener; the default
	// 90s kill timeout is generous, but 30s matches what the restart path waits
	// for and keeps a stuck shutdown from hanging a `systemctl restart`.
	b.WriteString("TimeoutStopSec=30\n")
	b.WriteString("\n[Install]\n")
	if user != "" {
		b.WriteString("WantedBy=multi-user.target\n")
	} else {
		b.WriteString("WantedBy=default.target\n")
	}
	return b.String()
}

// systemdQuote quotes a path for ExecStart.
//
// Paths with spaces are rare here but not impossible (~/.local/share under a
// home directory with a space). systemd splits ExecStart on whitespace and
// honours double quotes, so the only byte that needs escaping is " and \.
func systemdQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// --- systemctl plumbing -----------------------------------------------------

// findStableEntry looks for the `clawbench` command on PATH: the npm global
// prefix installs a shim there whose location is fixed even though the package
// it points at is versioned. Returns "" when there is none.
//
// A PATH entry may be relative, and systemd resolves ExecStart against its own
// idea of the filesystem, so the absolute form is what gets pinned.
var findStableEntry = func() string {
	p, err := exec.LookPath("clawbench")
	if err != nil {
		return ""
	}
	if abs, absErr := filepath.Abs(p); absErr == nil {
		return abs
	}
	return p
}

// runCommand is the single shell-out point, so tests can stub it.
var runCommand = func(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // argv-only, no shell; args are built here
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// hasSystemd reports whether systemctl exists and a manager answers.
func hasSystemd(system bool) bool {
	args := []string{"show-environment"}
	if !system {
		args = append([]string{"--user"}, args...)
	}
	if _, err := runCommand("systemctl", args...); err != nil {
		return false
	}
	return true
}

// --- the three actions ------------------------------------------------------

func runServiceInstall(args []string) int {
	// --binary overrides the path pinned into the unit. Ordinary installs do not
	// need it: the `clawbench` entry on PATH is already a stable location. It is
	// the escape hatch for a binary that was put somewhere deliberately outside
	// PATH.
	overrideBin := ""
	for i := 0; i < len(args); i++ {
		if (args[i] == "--binary" || args[i] == "-b") && i+1 < len(args) {
			overrideBin = args[i+1]
		} else if strings.HasPrefix(args[i], "--binary=") {
			overrideBin = strings.TrimPrefix(args[i], "--binary=")
		}
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "读不到当前程序路径: %v\n", err)
		return 1
	}
	// The pin target: an explicit --binary, else the fixed entry point on PATH.
	stableEntry := overrideBin
	if stableEntry == "" {
		stableEntry = findStableEntry()
	}
	if overrideBin != "" {
		abs, absErr := filepath.Abs(overrideBin)
		if absErr != nil {
			fmt.Fprintf(os.Stderr, "--binary 路径无效: %v\n", absErr)
			return 1
		}
		info, statErr := os.Stat(abs)
		if statErr != nil {
			fmt.Fprintf(os.Stderr, "--binary 指向的文件不存在: %v\n", statErr)
			return 1
		}
		if info.Mode()&0o111 == 0 {
			fmt.Fprintf(os.Stderr, "--binary 指向的文件没有执行权限: %s\n  chmod +x %s\n", abs, abs)
			return 1
		}
		if isVersionedInstallPath(abs) {
			fmt.Fprintln(os.Stderr, "--binary 指向的是 npm 的版本目录，升级后会失效。")
			fmt.Fprintln(os.Stderr, "不指定 --binary 时会自动改用 PATH 上那个固定入口；")
			fmt.Fprintf(os.Stderr, "或者换成一个固定路径：sudo cp %s /usr/local/bin/clawbench\n", abs)
			return 1
		}
		if overrideBin != "" {
			stableEntry = abs
		}
	}
	dataDir := model.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".clawbench")
	}
	isRoot := os.Geteuid() == 0
	isContainer := platform.IsContainer()
	managed := model.ManagedByEmbedder

	// 系统级探测和用户级探测分开：容器里 systemctl 也是存在的，只是不该用。
	exeResolved, outcome := decideServiceInstall(exe, stableEntry, dataDir, len(exe) > 0, isContainer, managed)
	if !outcome.ok {
		fmt.Fprintln(os.Stderr, "无法注册为服务：")
		fmt.Fprintln(os.Stderr, outcome.reason)
		return 1
	}

	systemdUser := hasSystemd(false)
	systemdSystem := hasSystemd(true)
	if isRoot {
		systemdUser = false
	}
	if !systemdUser && !systemdSystem {
		fmt.Fprintln(os.Stderr, "这台机器上没有可用的 systemd（systemctl 调用失败），装不了服务。")
		return 1
	}

	// 只有一条路可行就直接走，不拿多余的问题烦人。
	system := false
	switch {
	case systemdUser && systemdSystem:
		system = promptBool("装成系统级服务（开机就启动，需要 sudo）吗？选否则装成当前用户的服务（登录后启动）", isRoot)
	case systemdSystem:
		system = true
	default:
		system = false
	}

	home, homeErr := os.UserHomeDir()
	if homeErr != nil || home == "" {
		fmt.Fprintln(os.Stderr, "读不到 HOME，无法写服务文件。")
		return 1
	}
	user := ""
	if system {
		user = os.Getenv("SUDO_USER")
		if user == "" {
			u := os.Getenv("USER")
			if u == "" {
				u = "root"
			}
			user = u
		}
		if user != "root" {
			// 用 sudo 跑：HOME 现在是 /root，真实用户的家目录要另算。
			if h := lookupHome(user); h != "" {
				home = h
			}
		}
	}

	target := serviceTarget{system: system, user: user, home: home}
	if system {
		target.dir = "/etc/systemd/system"
	} else {
		target.dir = filepath.Join(home, ".config", "systemd", "user")
	}

	unit := buildUnitFile(exeResolved, dataDir, user, home)
	unitPath := filepath.Join(target.dir, serviceUnitName)

	fmt.Println("即将写入服务文件：")
	fmt.Println("  " + unitPath)
	fmt.Println()
	fmt.Println(unit)

	if !promptBool("继续吗", true) {
		fmt.Println("已取消。")
		return 1
	}

	if err := writeUnitFile(target, unitPath, unit); err != nil {
		if system && os.IsPermission(err) {
			// Not root: we cannot write /etc and we cannot ask for a sudo
			// password (no controlling terminal). Hand the two steps over
			// instead of failing with a bare permission error.
			fmt.Fprintln(os.Stderr, "没有写 /etc/systemd/system 的权限。请用 sudo 执行下面两条命令完成注册：")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "  sudo tee %s >/dev/null <<'EOF'\n%sEOF\n\n", unitPath, unit)
			fmt.Fprintf(os.Stderr, "  sudo systemctl daemon-reload && sudo systemctl enable --now %s\n", serviceUnitName)
			return 1
		}
		fmt.Fprintf(os.Stderr, "写服务文件失败: %v\n", err)
		return 1
	}

	prefix := []string{}
	if system {
		if os.Geteuid() != 0 {
			// 到这里说明用户选了系统级但不是 root：只能靠 sudo，而 sudo 需要
			// 密码，本进程没有终端密码框。把命令交出去，让用户自己执行。
			fmt.Println()
			fmt.Println("系统级服务需要 root 权限，请执行下面这条命令完成注册：")
			fmt.Println()
			fmt.Printf("  sudo systemctl daemon-reload && sudo systemctl enable --now %s\n", serviceUnitName)
			fmt.Println()
			fmt.Println("如果写文件也提示权限不足，请把上面的服务文件内容用 sudo 写到该路径。")
			return 0
		}
	} else {
		prefix = []string{"--user"}
	}

	if code := runSystemctl(prefix, "daemon-reload"); code != 0 {
		return code
	}
	if code := runSystemctl(prefix, "enable", "--now", serviceUnitName); code != 0 {
		return code
	}

	fmt.Println()
	fmt.Println("已注册。查看状态：clawbench service status")
	if !system {
		fmt.Println()
		fmt.Println("提示：用户级服务跟随登录，注销后就停了。想让它没登录时也跑，执行一次：")
		fmt.Printf("  sudo loginctl enable-linger %s\n", userOrFallback())
	}
	fmt.Println()
	fmt.Println("注意：当前这个手动启动的进程还占着数据目录和端口，请把它停掉，systemd 才能接管。")
	return 0
}

func runServiceUninstall() int {
	system := false
	if os.Geteuid() == 0 {
		system = true
	} else if _, err := os.Stat("/etc/systemd/system/" + serviceUnitName); err == nil {
		system = true
	}
	home, _ := os.UserHomeDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", serviceUnitName)
	prefix := []string{"--user"}
	if system {
		unitPath = "/etc/systemd/system/" + serviceUnitName
		prefix = nil
	}
	if _, err := os.Stat(unitPath); err != nil {
		fmt.Printf("没有找到服务文件（%s），可能本来就没装。\n", unitPath)
		return 0
	}
	runSystemctl(prefix, "disable", "--now", serviceUnitName)
	if err := os.Remove(unitPath); err != nil {
		fmt.Fprintf(os.Stderr, "删除 %s 失败: %v\n", unitPath, err)
		return 1
	}
	runSystemctl(prefix, "daemon-reload")
	fmt.Println("已卸载。数据目录没有动。")
	return 0
}

func runServiceStatus() int {
	home, _ := os.UserHomeDir()
	found := false
	for _, u := range []struct {
		label    string
		unitPath string
		prefix   []string
	}{
		{"用户级", filepath.Join(home, ".config", "systemd", "user", serviceUnitName), []string{"--user"}},
		{"系统级", "/etc/systemd/system/" + serviceUnitName, nil},
	} {
		if _, err := os.Stat(u.unitPath); err != nil {
			continue
		}
		found = true
		fmt.Printf("%s服务：已注册（%s）\n", u.label, u.unitPath)
		out, err := runCommand("systemctl", append(append([]string{}, u.prefix...), "is-active", serviceUnitName)...)
		if err != nil && out == "" {
			fmt.Println("  状态：查询失败（systemctl 不可用或没有权限）")
			continue
		}
		if out == "" {
			fmt.Println("  状态：未知（systemctl 没有返回结果）")
		} else {
			fmt.Printf("  状态：%s\n", out)
		}
		if detail, dErr := runCommand("systemctl", append(append([]string{}, u.prefix...), "show", "-p", "MainPID", serviceUnitName)...); dErr == nil && detail != "" {
			fmt.Println("  " + detail)
		}
	}
	if !found {
		fmt.Println("当前没有注册为 systemd 服务。注册：clawbench service install")
	}
	return 0
}

// --- helpers ----------------------------------------------------------------

func runSystemctl(prefix []string, args ...string) int {
	full := append(append([]string{}, prefix...), args...)
	if out, err := runCommand("systemctl", full...); err != nil {
		fmt.Fprintf(os.Stderr, "systemctl %s 失败: %v\n%s\n", strings.Join(full, " "), err, out)
		return 1
	}
	return 0
}

func writeUnitFile(t serviceTarget, path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// promptBool asks a yes/no question on stdin. A non-interactive stdin (piped,
// cron, CI) answers with the default instead of blocking forever.
func promptBool(question string, def bool) bool {
	defText := "Y/n"
	if !def {
		defText = "y/N"
	}
	fmt.Printf("%s [%s] ", question, defText)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return def
	}
	line = strings.ToLower(strings.TrimSpace(line))
	switch line {
	case "y", "yes", "是":
		return true
	case "n", "no", "否":
		return false
	case "":
		return def
	}
	return def
}

// lookupHome resolves a user's home directory via getent, avoiding cgo.
func lookupHome(user string) string {
	out, err := runCommand("getent", "passwd", user)
	if err != nil {
		return ""
	}
	parts := strings.Split(out, ":")
	if len(parts) < 6 {
		return ""
	}
	return parts[5]
}

func userOrFallback() string {
	if u := os.Getenv("SUDO_USER"); u != "" {
		return u
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "$(id -un)"
}

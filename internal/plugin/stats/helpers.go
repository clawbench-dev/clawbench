package stats

import "os/exec"

// isGitRepo checks if the given path is inside a git repository.
//
// 与 handler/git.go 的同名函数一致：仓库浏览接口和统计接口必须对「这是不是
// 一个 git 项目」给出同一个答案。
func isGitRepo(projectPath string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = projectPath
	_, err := cmd.Output()
	return err == nil
}

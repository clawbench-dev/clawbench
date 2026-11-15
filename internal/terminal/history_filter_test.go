package terminal

import "testing"

func TestIsSensitiveCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"git status", false},
		{"ls -la /tmp", false},
		{"mkdir -p build/out", false},
		{"ssh -p 2222 host", false},
		{"docker run -p 8080:80 nginx", false},
		{"export OPENAI_KEY=sk-abc123", true},
		{"GH_TOKEN=ghp_xxx gh pr list", true},
		{"DB_PASSWORD=hunter2 ./app", true},
		{"export MY_SECRET=abc", true},
		{"curl -H 'Authorization: Bearer abc' https://x", true},
		{"mysql --password=root -u root", true},
		{"git config --global user.password=abc", true},
		{"npm run build", false},
		{"echo hello", false},
	}
	for _, c := range cases {
		if got := IsSensitiveCommand(c.cmd); got != c.want {
			t.Errorf("IsSensitiveCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestLooksLikeCommand(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"ls -la", true},
		{"cd /tmp", true},
		{"export FOO=bar", true},
		{"LC_ALL=C grep foo file", true},
		{"sh -c 'echo hi'", true},
		{"", false},
		{"   ", false},
		{":wq", false},
		{":q!", false},
		{"/needle", false},
		{"?backwards", false},
		{"definitely-not-a-real-command-xyz", false},
		{"./definitely-not-a-real-script.sh", false},
		{"line with \x01 control", false},
	}
	for _, c := range cases {
		if got := LooksLikeCommand(c.line); got != c.want {
			t.Errorf("LooksLikeCommand(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestLooksLikeCommand_RejectsOverlongInput(t *testing.T) {
	long := make([]byte, maxCommandLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if LooksLikeCommand(string(long)) {
		t.Error("overlong line should not be treated as a command")
	}
}

func TestFirstCommandWord(t *testing.T) {
	cases := []struct{ line, want string }{
		{"ls -la", "ls"},
		{"A=1 B=2 make all", "make"},
		{"'quoted cmd' arg", "quoted cmd"},
		{"git commit -m 'x y'", "git"},
		{"FOO=bar; ls", ""},
	}
	for _, c := range cases {
		if got := firstCommandWord(c.line); got != c.want {
			t.Errorf("firstCommandWord(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

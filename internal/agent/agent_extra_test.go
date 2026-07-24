package agent

import (
	"context"
	"errors"
	"os"
	"testing"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/session"
)

func TestRun_NilHistory(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), Options{}, nil, scriptedSend(t, []string{"x"}), func(string) {})
	if err == nil {
		t.Fatal("expected error for nil history")
	}
}

func TestRun_NilSendFunc(t *testing.T) {
	t.Parallel()
	hist := session.NewHistory("ctx")
	err := Run(context.Background(), Options{}, hist, nil, func(string) {})
	if err == nil {
		t.Fatal("expected error for nil send")
	}
}

func TestRun_NilNotifyFunc(t *testing.T) {
	t.Parallel()
	hist := session.NewHistory("ctx")
	err := Run(context.Background(), Options{}, hist, scriptedSend(t, []string{"x"}), nil)
	if err == nil {
		t.Fatal("expected error for nil notify")
	}
}

func TestParseGoal_EmptyGoal(t *testing.T) {
	t.Parallel()
	goal, max := ParseGoal("")
	if goal != "" {
		t.Errorf("expected empty goal, got %q", goal)
	}
	if max != MaxIterations {
		t.Errorf("expected MaxIterations, got %d", max)
	}
}

func TestParseGoal_WithIterationCount2(t *testing.T) {
	t.Parallel()
	goal, max := ParseGoal("5 iterations fix the bugs")
	if goal != "fix the bugs" {
		t.Errorf("expected 'fix the bugs', got %q", goal)
	}
	if max != 5 {
		t.Errorf("expected 5, got %d", max)
	}
}

func TestParseGoal_MaxIterations(t *testing.T) {
	t.Parallel()
	goal, max := ParseGoal("max 10 iterations do something")
	if goal != "do something" {
		t.Errorf("expected 'do something', got %q", goal)
	}
	if max != 10 {
		t.Errorf("expected 10, got %d", max)
	}
}

func TestParseGoal_NoMatch2(t *testing.T) {
	t.Parallel()
	goal, max := ParseGoal("fix the bugs")
	if goal != "fix the bugs" {
		t.Errorf("expected 'fix the bugs', got %q", goal)
	}
	if max != MaxIterations {
		t.Errorf("expected MaxIterations, got %d", max)
	}
}

func TestParseGoal_InvalidNumber2(t *testing.T) {
	t.Parallel()
	goal, max := ParseGoal("abc iterations do something")
	if goal != "abc iterations do something" {
		t.Errorf("expected original string, got %q", goal)
	}
	if max != MaxIterations {
		t.Errorf("expected MaxIterations, got %d", max)
	}
}

func TestRun_FileWriteRefused2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hist := session.NewHistory("ctx")
	send := scriptedSend(t, []string{
		"```file:../../etc/passwd\nroot:x:0:0:root:/root:/bin/bash\n```\nAUTO_DONE",
	})
	notify, _ := collectNotify()

	err := Run(context.Background(), Options{
		Goal:    "write outside sandbox",
		Sandbox: true,
		WorkDir: dir,
	}, hist, send, notify)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRun_CommandSandbox2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hist := session.NewHistory("ctx")
	send := scriptedSend(t, []string{
		"```run\necho hello\n```\nAUTO_DONE",
	})
	notify, _ := collectNotify()

	err := Run(context.Background(), Options{
		Goal:    "echo hello",
		Sandbox: true,
		WorkDir: dir,
	}, hist, send, notify)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRun_NonSandboxCommand2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hist := session.NewHistory("ctx")
	send := scriptedSend(t, []string{
		"```run\necho hello\n```\nAUTO_DONE",
	})
	notify, _ := collectNotify()

	err := Run(context.Background(), Options{
		Goal:    "echo hello",
		Sandbox: false,
		WorkDir: dir,
	}, hist, send, notify)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestMatchDangerousRM_RmForceRecursive2(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{"rm -rf /", "rm -rf /", true},
		{"rm -rf *", "rm -rf *", true},
		{"rm -fr ~", "rm -fr ~", true},
		{"rm -rf $HOME", "rm -rf $HOME", true},
		{"rm -f file", "rm -f file", false},
		{"rm file", "rm file", false},
		{"rm -rf /tmp/test", "rm -rf /tmp/test", true},
		{"ls -la", "ls -la", false},
		{"rm --recursive --force /", "rm --recursive --force /", true},
		{"rm --force file", "rm --force file", false},
		{"rm -Rf /", "rm -Rf /", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchDangerousRM(c.cmd)
			if got != c.want {
				t.Errorf("matchDangerousRM(%q) = %v, want %v", c.cmd, got, c.want)
			}
		})
	}
}

func TestResolveWritePath_Placeholder2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := resolveWritePath(dir, "RELATIVE_PATH_TO_THE_FILE")
	if err == nil {
		t.Fatal("expected error for placeholder path")
	}
}

func TestResolveWritePath_AngleBracket2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := resolveWritePath(dir, "<template>")
	if err == nil {
		t.Fatal("expected error for angle bracket path")
	}
}

func TestResolveWritePath_Valid2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := resolveWritePath(dir, "test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestRun_SendError2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hist := session.NewHistory("ctx")
	send := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		return "", "", errors.New("connection refused")
	}
	notify, _ := collectNotify()

	err := Run(context.Background(), Options{
		Goal:    "fail",
		Sandbox: true,
		WorkDir: dir,
	}, hist, send, notify)
	if err == nil {
		t.Fatal("expected error from send")
	}
}

func TestRun_MalformedBlockWarning2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hist := session.NewHistory("ctx")
	send := scriptedSend(t, []string{
		"```file:main.go\npackage main\n",
		"Ok, making a real change.\n```run\necho done\n```\nAUTO_DONE",
		"No really, done.\nAUTO_DONE",
	})
	notify, lines := collectNotify()

	err := Run(context.Background(), Options{
		Goal:    "fix code",
		Sandbox: true,
		WorkDir: dir,
	}, hist, send, notify)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	joined := ""
	for _, l := range *lines {
		joined += l + "\n"
	}
	if !contains(joined, "warning: possible malformed block") {
		t.Errorf("expected malformed block warning in output")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestShPath2(t *testing.T) {
	t.Parallel()
	p := shPath()
	if p == "" {
		t.Fatal("expected non-empty sh path")
	}
}

func TestWriteFile_MkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	// Create a file where the parent directory should be
	blocker := dir + "/blocker"
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Try to write to blocker/subdir/file.txt — MkdirAll will fail
	// because "blocker" is a file, not a directory
	err := writeFile(blocker+"/subdir/file.txt", "content")
	if err == nil {
		t.Fatal("expected error when parent path is blocked by a file")
	}
}

func TestShPath_BashNotFound(t *testing.T) {
	origPath := os.Getenv("PATH")
	origBin := os.Getenv("OLLAMA_BIN")
	os.Setenv("PATH", "/nonexistent")
	os.Setenv("OLLAMA_BIN", "/nonexistent/ollama")
	defer func() {
		os.Setenv("PATH", origPath)
		os.Setenv("OLLAMA_BIN", origBin)
	}()

	// Only run if /bin/sh exists (it should on Linux)
	if _, err := os.Stat("/bin/sh"); os.IsNotExist(err) {
		t.Skip("/bin/sh not found")
	}
	p := shPath()
	if p != "/bin/sh" {
		t.Errorf("expected /bin/sh fallback, got %q", p)
	}
}

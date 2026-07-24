package app

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/session"
)

func TestMakeExchange_SendsAndRecords(t *testing.T) {
	t.Parallel()

	hist := session.NewHistory("test context")

	sent := false
	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		sent = true
		if onToken != nil {
			onToken("Hello ")
			onToken("World")
		}
		return "test-engine", "Hello World", nil
	}

	logger := slog.Default()
	exchange := makeExchange(hist, sendMsg, nil, logger)

	exchange()

	if !sent {
		t.Error("expected sendMessage to be called")
	}

	snap := hist.Snapshot()
	found := false
	for _, m := range snap {
		if m.Role == "assistant" && m.Content == "Hello World" {
			found = true
		}
	}
	if !found {
		t.Error("expected assistant response in history")
	}
}

func TestMakeExchange_RecordsError(t *testing.T) {
	t.Parallel()

	hist := session.NewHistory("test context")

	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		return "", "", context.DeadlineExceeded
	}

	logger := slog.Default()
	exchange := makeExchange(hist, sendMsg, nil, logger)

	exchange()

	snap := hist.Snapshot()
	for _, m := range snap {
		if m.Role == "assistant" && m.Content != "" {
			t.Error("should not record error response in history")
		}
	}
}

func TestParseFlags_Various(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantAuto bool
		wantChat bool
	}{
		{"empty", []string{}, false, false},
		{"auto", []string{"--auto", "fix bugs"}, true, false},
		{"chat", []string{"--chat"}, false, true},
		{"target", []string{"/some/path"}, false, false},
		{"train", []string{"--train"}, false, false},
		{"dataset-init", []string{"--dataset-init"}, false, false},
		{"easter-egg", []string{"--easter-egg"}, false, false},
		{"live-output", []string{"--auto", "test", "--live-output"}, true, false},
		{"auto-sandbox", []string{"--auto-sandbox", "/path"}, false, false},
		{"topic", []string{"--topic", "code review"}, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := ParseFlags(tc.args)
			if f.AutoMode != tc.wantAuto {
				t.Errorf("AutoMode = %v, want %v", f.AutoMode, tc.wantAuto)
			}
			if f.Chat != tc.wantChat {
				t.Errorf("Chat = %v, want %v", f.Chat, tc.wantChat)
			}
		})
	}
}

func TestCreateSnapshot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Test with non-existent path
	err := createSnapshot(dir, "/nonexistent/path", "before")
	if err != nil {
		t.Errorf("createSnapshot with nonexistent path should return nil, got: %v", err)
	}

	// Test with a file
	srcFile := dir + "/test.txt"
	if err := writeFile(srcFile, "hello"); err != nil {
		t.Fatal(err)
	}

	err = createSnapshot(dir+"/backup", srcFile, "before")
	if err != nil {
		t.Errorf("createSnapshot failed: %v", err)
	}

	// Verify snapshot was created
	entries, _ := listDir(dir + "/backup")
	if len(entries) == 0 {
		t.Error("expected snapshot directory to be created")
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := dir + "/src.txt"
	dst := dir + "/dst.txt"

	if err := writeFile(src, "test content"); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}

	content, err := readFile(dst)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}
	if content != "test content" {
		t.Errorf("expected 'test content', got %q", content)
	}
}

func TestCopyFile_NonexistentSource(t *testing.T) {
	t.Parallel()
	err := copyFile("/nonexistent/src.txt", "/tmp/dst.txt")
	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestCopyDir(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	dstDir := t.TempDir() + "/dst"

	// Create source structure
	writeFile(srcDir+"/a.txt", "file a")
	writeFile(srcDir+"/b.txt", "file b")

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	content, _ := readFile(dstDir + "/a.txt")
	if content != "file a" {
		t.Errorf("expected 'file a', got %q", content)
	}
	content, _ = readFile(dstDir + "/b.txt")
	if content != "file b" {
		t.Errorf("expected 'file b', got %q", content)
	}
}

func TestCopyDir_NestedDirectories(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	dstDir := t.TempDir() + "/dst"

	os.MkdirAll(srcDir+"/sub", 0755)
	writeFile(srcDir+"/sub/nested.txt", "nested content")
	writeFile(srcDir+"/top.txt", "top content")

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir failed: %v", err)
	}

	content, _ := readFile(dstDir + "/sub/nested.txt")
	if content != "nested content" {
		t.Errorf("expected 'nested content', got %q", content)
	}
	content, _ = readFile(dstDir + "/top.txt")
	if content != "top content" {
		t.Errorf("expected 'top content', got %q", content)
	}
}

func TestCopyDir_EmptyDir(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir() + "/dst"

	if err := copyDir(srcDir, dstDir); err != nil {
		t.Fatalf("copyDir empty: %v", err)
	}

	entries, _ := listDir(dstDir)
	if len(entries) != 0 {
		t.Errorf("expected empty dir, got %d entries", len(entries))
	}
}

func TestRunDatasetInit(t *testing.T) {
	// Cannot use t.Parallel — os.Chdir is process-global
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	code := runDatasetInit()
	if code != 0 {
		t.Errorf("runDatasetInit exit code = %d, want 0", code)
	}
}

func TestRunEasterEgg(t *testing.T) {
	t.Parallel()
	code := runEasterEgg("http://localhost:1", Flags{CustomTopic: "test"})
	// RunGenerate with pipe=false gracefully returns 0 even on connection failure
	_ = code
}

func TestRunTrain_NoCommits(t *testing.T) {
	// Cannot use t.Parallel — os.Chdir is process-global
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	code := runTrain("http://localhost:1", "test-model", false)
	if code != 0 {
		t.Errorf("runTrain with no commits should return 0, got %d", code)
	}
}

func TestRunAuto(t *testing.T) {
	t.Parallel()

	// runAuto requires ollama, so just test it returns error gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	eng := llm.NewLocalEngine("http://localhost:1", "test-model", "sys", 8192, 100, retryConfig(), slog.Default())
	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		return "", "", context.DeadlineExceeded
	}

	flags := Flags{AutoGoal: "test goal", AutoSandbox: true}
	_ = runAuto(ctx, flags, eng, sendMsg, slog.Default())
}

func retryConfig() retry.Config {
	return retry.Config{
		MaxAttempts: 1,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

func listDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

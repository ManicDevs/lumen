package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/session"
)

func TestCopyFile_SourceNotExist2(t *testing.T) {
	t.Parallel()
	err := copyFile("/nonexistent/file.txt", "/tmp/dest.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCopyFile_Success2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "sub", "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", string(data), "hello")
	}
}

func TestCopyDir_Success2(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "copy")
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644)
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("b"), 0644)

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Error("expected a.txt in copy")
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "b.txt")); err != nil {
		t.Error("expected sub/b.txt in copy")
	}
}

func TestCreateSnapshot_NonexistentPath2(t *testing.T) {
	t.Parallel()
	err := createSnapshot(t.TempDir(), "/nonexistent/path", "before")
	if err != nil {
		t.Errorf("expected nil for nonexistent path, got: %v", err)
	}
}

func TestCreateSnapshot_Directory2(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "source")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "file.go"), []byte("content"), 0644)

	backupDir := filepath.Join(dir, "backups")
	os.MkdirAll(backupDir, 0755)

	err := createSnapshot(backupDir, srcDir, "before")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, _ := os.ReadDir(backupDir)
	if len(entries) == 0 {
		t.Error("expected snapshot directory to be created")
	}
}

func TestRun_EasterEgg2(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	srv := mockOllama(t)
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)

	code := Run([]string{"--easter-egg"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func mockAutoOllama(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunk, _ := json.Marshal(map[string]interface{}{
			"message": map[string]string{"role": "assistant", "content": "done now\nAUTO_DONE"},
			"done":    true,
		})
		w.Write(chunk)
		w.Write([]byte("\n"))
	})
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		chunk, _ := json.Marshal(map[string]interface{}{"response": "ok", "done": true})
		w.Write(chunk)
		w.Write([]byte("\n"))
	})
	return httptest.NewServer(mux)
}

func TestRun_AutoMode2(t *testing.T) {
	srv := mockAutoOllama(t)
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	code := Run([]string{"--auto", "echo hello"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_AutoSandbox2(t *testing.T) {
	srv := mockAutoOllama(t)
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	code := Run([]string{"--auto", "goal", "--auto-sandbox"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_TrainWithInvalidHost2(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	t.Setenv("OLLAMA_HOST", "http://localhost:1")

	code := Run([]string{"--train"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestParseFlags_AllFlags2(t *testing.T) {
	t.Parallel()
	f := ParseFlags([]string{
		"--auto", "my goal",
		"--auto-sandbox",
		"--live-output",
		"--easter-egg",
		"--continuous",
		"--pipe-dataset",
		"--train",
		"--dataset-init",
		"--chat",
		"--topic", "my topic",
	})
	if !f.AutoMode {
		t.Error("expected AutoMode")
	}
	if f.AutoGoal != "my goal" {
		t.Errorf("AutoGoal = %q, want %q", f.AutoGoal, "my goal")
	}
	if !f.AutoSandbox {
		t.Error("expected AutoSandbox")
	}
	if !f.LiveOutput {
		t.Error("expected LiveOutput")
	}
	if !f.EasterEgg {
		t.Error("expected EasterEgg")
	}
	if !f.Continuous {
		t.Error("expected Continuous")
	}
	if !f.PipeDataset {
		t.Error("expected PipeDataset")
	}
	if !f.Train {
		t.Error("expected Train")
	}
	if !f.DatasetInit {
		t.Error("expected DatasetInit")
	}
	if !f.Chat {
		t.Error("expected Chat")
	}
	if f.CustomTopic != "my topic" {
		t.Errorf("CustomTopic = %q, want %q", f.CustomTopic, "my topic")
	}
}

func TestParseFlags_TrainAll2(t *testing.T) {
	t.Parallel()
	f := ParseFlags([]string{"--train-all"})
	if !f.Train {
		t.Error("expected Train")
	}
	if !f.TrainAll {
		t.Error("expected TrainAll")
	}
}

func TestParseFlags_Autonomous2(t *testing.T) {
	t.Parallel()
	f := ParseFlags([]string{"--autonomous"})
	if !f.Continuous {
		t.Error("expected Continuous with --autonomous")
	}
}

func TestParseFlags_TargetPath2(t *testing.T) {
	t.Parallel()
	f := ParseFlags([]string{"/path/to/file"})
	if f.TargetPath != "/path/to/file" {
		t.Errorf("TargetPath = %q, want %q", f.TargetPath, "/path/to/file")
	}
}

func TestParseFlags_UnknownFlag2(t *testing.T) {
	t.Parallel()
	f := ParseFlags([]string{"--unknown-flag"})
	if f.TargetPath != "" {
		t.Errorf("TargetPath should be empty, got %q", f.TargetPath)
	}
}

func TestParseFlags_AutoNoGoal2(t *testing.T) {
	t.Parallel()
	f := ParseFlags([]string{"--auto"})
	if !f.AutoMode {
		t.Error("expected AutoMode")
	}
	if f.AutoGoal != "" {
		t.Errorf("AutoGoal should be empty, got %q", f.AutoGoal)
	}
}

func TestParseFlags_TopicNoValue2(t *testing.T) {
	t.Parallel()
	f := ParseFlags([]string{"--topic"})
	if f.CustomTopic != "" {
		t.Errorf("CustomTopic should be empty, got %q", f.CustomTopic)
	}
}

func TestMakeExchange_WithAuditLogAndTokens2(t *testing.T) {
	t.Parallel()
	hist := session.NewHistory("test")
	logger := slog.Default()
	dir := t.TempDir()
	auditLog, err := session.OpenAuditLog(dir+"/audit.jsonl", logger)
	if err != nil {
		t.Fatal(err)
	}
	defer auditLog.Close()

	sendMsg := func(ctx context.Context, history []llm.ChatMessage, onToken llm.StreamFunc) (string, string, error) {
		if onToken != nil {
			onToken("tok1")
			onToken("tok2")
		}
		return "engine", "response", nil
	}
	exchange := makeExchange(hist, sendMsg, auditLog, logger)
	exchange()

	snap := hist.Snapshot()
	found := false
	for _, m := range snap {
		if m.Role == "assistant" && m.Content == "response" {
			found = true
		}
	}
	if !found {
		t.Error("expected assistant response in history")
	}
}

func TestRun_InteractiveWithUserInput2(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("hello\n")
		time.Sleep(50 * time.Millisecond)
		pw.WriteString("exit\n")
		pw.Close()
	}()

	code := Run([]string{})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_TargetPath2(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "test.go")
	os.WriteFile(srcFile, []byte("package main\n\nfunc main() {}\n"), 0644)

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("exit\n")
		pw.Close()
	}()

	code := Run([]string{srcFile})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_TargetPathInvalid2(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	code := Run([]string{"/nonexistent/path"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_ChatMode2(t *testing.T) {
	srv := mockOllama(t)
	defer srv.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("hello\n")
		time.Sleep(50 * time.Millisecond)
		pw.WriteString("exit\n")
		pw.Close()
	}()

	code := Run([]string{"--chat"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_ConfigInvalidFormat2(t *testing.T) {
	t.Setenv("LOG_FORMAT", "XML")
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")
	code := Run([]string{})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_ConfigInvalidHost2(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "not-a-url")
	code := Run([]string{})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_EasterEggError2(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")

	code := Run([]string{"--easter-egg"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_TrainAllError2(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")

	code := Run([]string{"--train-all"})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRun_InteractiveWithAutoSlash2(t *testing.T) {
	srv := mockAutoOllama(t)
	defer srv.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = pr
	defer func() { os.Stdin = origStdin }()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	go func() {
		time.Sleep(100 * time.Millisecond)
		pw.WriteString("/auto do something\n")
		time.Sleep(200 * time.Millisecond)
		pw.WriteString("exit\n")
		pw.Close()
	}()

	code := Run([]string{})
	_ = code
}

func TestRun_ConfigInvalidModel2(t *testing.T) {
	t.Setenv("LOG_LEVEL", "invalid-level")
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")
	code := Run([]string{})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_DatasetInitError(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	// Create a file where the commits directory should be, so MkdirAll fails
	if err := os.MkdirAll("data/datasets", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/datasets/commits", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	code := Run([]string{"--dataset-init"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_TrainError2(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")
	t.Setenv("LUMEN_LOG_LEVEL", "warn")

	// Create a commit so RunTrain actually tries to create a model
	commitsDir := filepath.Join("data", "datasets", "commits")
	os.MkdirAll(commitsDir, 0755)
	commitData := `{"datapoints":[{"prompt":"test","response":"data"}]}`
	os.WriteFile(filepath.Join(commitsDir, "commit_001.json"), []byte(commitData), 0644)

	code := Run([]string{"--train"})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

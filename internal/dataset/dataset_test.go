package dataset

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildModelfile(t *testing.T) {
	t.Parallel()
	msgs := []Datapoint{
		{Prompt: "What is 2+2?", Response: "4"},
		{Prompt: "What is 3+3?", Response: "6"},
	}
	result := buildModelfile("qwen2.5-coder:3b", msgs)

	if !strings.HasPrefix(result, "FROM qwen2.5-coder:3b\n") {
		t.Error("expected FROM line")
	}
	if !strings.Contains(result, "SYSTEM") {
		t.Error("expected SYSTEM line")
	}
	if !strings.Contains(result, "MESSAGE user") {
		t.Error("expected MESSAGE user lines")
	}
	if !strings.Contains(result, "MESSAGE assistant") {
		t.Error("expected MESSAGE assistant lines")
	}
	if !strings.Contains(result, "What is 2+2?") {
		t.Error("expected first prompt")
	}
	if !strings.Contains(result, "What is 3+3?") {
		t.Error("expected second prompt")
	}
}

func TestBuildModelfile_EmptyMessages(t *testing.T) {
	t.Parallel()
	result := buildModelfile("base", nil)
	if !strings.HasPrefix(result, "FROM base\n") {
		t.Error("expected FROM line even with no messages")
	}
	if strings.Contains(result, "MESSAGE") {
		t.Error("should not contain MESSAGE lines when empty")
	}
}

func TestQuoteModelfileString_Simple(t *testing.T) {
	t.Parallel()
	got := quoteModelfileString("hello")
	if got != `"""hello"""` {
		t.Errorf("quoteModelfileString = %q", got)
	}
}

func TestQuoteModelfileString_WithTripleQuotes(t *testing.T) {
	t.Parallel()
	got := quoteModelfileString(`text with """ inside`)
	if !strings.Contains(got, `\"\"\"`) {
		t.Errorf("should escape triple quotes, got %q", got)
	}
	if !strings.HasPrefix(got, `"""`) || !strings.HasSuffix(got, `"""`) {
		t.Errorf("should be wrapped in triple quotes, got %q", got)
	}
}

func TestQuoteModelfileString_Empty(t *testing.T) {
	t.Parallel()
	got := quoteModelfileString("")
	if got != `""""""` {
		t.Errorf("empty string: got %q", got)
	}
}

func TestDirExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if !dirExists(dir) {
		t.Error("existing dir should return true")
	}
	if dirExists(filepath.Join(dir, "nonexistent")) {
		t.Error("nonexistent dir should return false")
	}
}

func TestCreateOllamaModel_Success(t *testing.T) {
	t.Parallel()
	var received ollamaCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/create" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ollamaCreateStatus{Status: "success"})
	}))
	defer srv.Close()

	err := createOllamaModel(srv.URL, "test-model", "FROM base")
	if err != nil {
		t.Fatalf("createOllamaModel: %v", err)
	}
	if received.Model != "test-model" {
		t.Errorf("Model = %q", received.Model)
	}
	if received.Modelfile != "FROM base" {
		t.Errorf("Modelfile = %q", received.Modelfile)
	}
}

func TestCreateOllamaModel_Non200(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	err := createOllamaModel(srv.URL, "test", "FROM base")
	if err == nil {
		t.Error("expected error for non-200 response")
	}
}

func TestCreateOllamaModel_OllamaError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ollamaCreateStatus{Error: "model not found"})
	}))
	defer srv.Close()

	err := createOllamaModel(srv.URL, "test", "FROM base")
	if err == nil {
		t.Error("expected error for Ollama error response")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error should contain Ollama error, got: %v", err)
	}
}

func TestCreateOllamaModel_NetworkError(t *testing.T) {
	t.Parallel()
	err := createOllamaModel("http://localhost:1", "test", "FROM base")
	if err == nil {
		t.Error("expected error for unreachable host")
	}
	if !strings.Contains(err.Error(), "network error") {
		t.Errorf("error should mention network error, got: %v", err)
	}
}

func TestRunInit(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	err := RunInit()
	if err != nil {
		t.Fatalf("RunInit: %v", err)
	}

	// Verify directories exist under data/datasets/
	root := filepath.Join(dir, "data", "datasets")
	for _, d := range []string{"commits", "stage", "refs", "refs/heads"} {
		path := filepath.Join(root, d)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", d)
		}
	}
	keepPath := filepath.Join(root, "stage", ".gitkeep")
	if _, err := os.Stat(keepPath); os.IsNotExist(err) {
		t.Error("expected stage/.gitkeep to exist")
	}
}

func TestRunInit_Idempotent(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	RunInit()
	RunInit()

	commitsDir := filepath.Join(dir, "data", "datasets", "commits")
	if _, err := os.Stat(commitsDir); os.IsNotExist(err) {
		t.Error("commits dir should exist after double init")
	}
}

func TestWriteCommit(t *testing.T) {
	dir := t.TempDir()
	commitsDir := filepath.Join(dir, "commits")
	refsPath := filepath.Join(dir, "refs", "heads", "master")
	os.MkdirAll(commitsDir, 0755)
	os.MkdirAll(filepath.Dir(refsPath), 0755)

	datapoints := []Datapoint{
		{Prompt: "p1", Response: "r1"},
		{Prompt: "p2", Response: "r2"},
	}

	commitID, err := writeCommit(commitsDir, refsPath, "test-model", datapoints)
	if err != nil {
		t.Fatalf("writeCommit: %v", err)
	}
	if commitID == "" {
		t.Error("commitID should not be empty")
	}

	// Verify commit file exists
	commitFile := filepath.Join(commitsDir, "commit_"+commitID+".json")
	data, err := os.ReadFile(commitFile)
	if err != nil {
		t.Fatalf("reading commit file: %v", err)
	}

	var commit Commit
	if err := json.Unmarshal(data, &commit); err != nil {
		t.Fatalf("unmarshal commit: %v", err)
	}
	if len(commit.Datapoints) != 2 {
		t.Errorf("expected 2 datapoints, got %d", len(commit.Datapoints))
	}
	if commit.Model != "test-model" {
		t.Errorf("Model = %q", commit.Model)
	}

	// Verify ref pointer
	refData, err := os.ReadFile(refsPath)
	if err != nil {
		t.Fatalf("reading ref: %v", err)
	}
	var ref RefPointer
	if err := json.Unmarshal(refData, &ref); err != nil {
		t.Fatalf("unmarshal ref: %v", err)
	}
	if ref.LatestCommit != commitID {
		t.Errorf("LatestCommit = %q, want %q", ref.LatestCommit, commitID)
	}
	if ref.TotalCommits != 1 {
		t.Errorf("TotalCommits = %d, want 1", ref.TotalCommits)
	}
}

func TestWriteCommit_IncrementsRef(t *testing.T) {
	dir := t.TempDir()
	commitsDir := filepath.Join(dir, "commits")
	refsPath := filepath.Join(dir, "refs", "heads", "master")
	os.MkdirAll(commitsDir, 0755)
	os.MkdirAll(filepath.Dir(refsPath), 0755)

	id1, _ := writeCommit(commitsDir, refsPath, "m", []Datapoint{{Prompt: "p1", Response: "r1"}})
	id2, _ := writeCommit(commitsDir, refsPath, "m", []Datapoint{{Prompt: "p2", Response: "r2"}})

	refData, _ := os.ReadFile(refsPath)
	var ref RefPointer
	json.Unmarshal(refData, &ref)

	if ref.LatestCommit != id2 {
		t.Errorf("LatestCommit = %q, want %q", ref.LatestCommit, id2)
	}
	if ref.TotalCommits != 2 {
		t.Errorf("TotalCommits = %d, want 2", ref.TotalCommits)
	}
	if id1 == id2 {
		t.Error("commit IDs should differ")
	}
}

func mockOllamaServer(t *testing.T, responses ...string) *httptest.Server {
	t.Helper()
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher := w.(http.Flusher)
		if idx < len(responses) {
			resp := responses[idx]
			idx++
			chunk := map[string]any{
				"message": map[string]string{"role": "assistant", "content": resp},
				"done":    false,
			}
			data, _ := json.Marshal(chunk)
			w.Write(append(data, '\n'))
			flusher.Flush()
			done := map[string]any{
				"message": map[string]string{"role": "assistant", "content": ""},
				"done":    true,
			}
			doneData, _ := json.Marshal(done)
			w.Write(append(doneData, '\n'))
			flusher.Flush()
		} else {
			done := map[string]any{
				"message": map[string]string{"role": "assistant", "content": "default reply"},
				"done":    true,
			}
			doneData, _ := json.Marshal(done)
			w.Write(append(doneData, '\n'))
			flusher.Flush()
		}
	}))
}

func TestRunGenerate_SinglePass(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	srv := mockOllamaServer(t, "This is a test response with ### heading")
	defer srv.Close()

	err := RunGenerate("test-model", srv.URL, false, false, "test topic")
	if err != nil {
		t.Errorf("RunGenerate single pass: %v", err)
	}
}

func TestRunGenerate_WithPipe(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	srv := mockOllamaServer(t, "Response with ### structure")
	defer srv.Close()

	err := RunGenerate("test-model", srv.URL, false, true, "test topic")
	if err != nil {
		t.Errorf("RunGenerate with pipe: %v", err)
	}

	// Verify commit was written
	commits, _ := filepath.Glob(filepath.Join(dir, DatasetRoot, "commits", "commit_*.json"))
	if len(commits) != 1 {
		t.Errorf("expected 1 commit file, got %d", len(commits))
	}
}

func TestRunGenerate_EmptyTopic(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	srv := mockOllamaServer(t, "Response ### Heading")
	defer srv.Close()

	// Empty topic should pick a random seed topic
	err := RunGenerate("test-model", srv.URL, false, false, "")
	if err != nil {
		t.Errorf("RunGenerate empty topic: %v", err)
	}
}

func TestRunGenerate_ConnectionFailure(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	err := RunGenerate("test-model", "http://localhost:1", false, false, "test")
	if err != nil {
		t.Errorf("RunGenerate connection failure should return nil, got: %v", err)
	}
}

func TestRunTrain_NoCommits(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	err := RunTrain("http://localhost:1", "base-model", false)
	if err != nil {
		t.Errorf("RunTrain no commits: %v", err)
	}
}

func TestRunTrain_WithCommits(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	// Create dataset structure
	commitsDir := filepath.Join(dir, DatasetRoot, "commits")
	refsPath := filepath.Join(dir, DatasetRoot, "refs", "heads", "master")
	os.MkdirAll(commitsDir, 0755)
	os.MkdirAll(filepath.Dir(refsPath), 0755)

	// Write a commit with datapoints
	commit := Commit{
		CommitID:  "test123",
		Timestamp: "2025-01-01T00:00:00Z",
		Model:     "test",
		Datapoints: []Datapoint{
			{Prompt: "What is 2+2?", Response: "4. It's basic arithmetic."},
			{Prompt: "What is 3+3?", Response: "6. Another simple calculation."},
		},
	}
	data, _ := json.MarshalIndent(commit, "", "  ")
	os.WriteFile(filepath.Join(commitsDir, "commit_test123.json"), data, 0644)

	// Mock ollama create endpoint
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/create" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(ollamaCreateStatus{Status: "success"})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	err := RunTrain(srv.URL, "base-model", false)
	if err != nil {
		t.Errorf("RunTrain with commits: %v", err)
	}

	// Verify archived commit
	trainedDir := filepath.Join(commitsDir, "trained")
	archived, _ := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))
	if len(archived) != 1 {
		t.Errorf("expected 1 archived commit, got %d", len(archived))
	}
}

func TestRunTrain_UseAll(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	commitsDir := filepath.Join(dir, DatasetRoot, "commits")
	trainedDir := filepath.Join(commitsDir, "trained")
	refsPath := filepath.Join(dir, DatasetRoot, "refs", "heads", "master")
	os.MkdirAll(trainedDir, 0755)
	os.MkdirAll(filepath.Dir(refsPath), 0755)

	// Write an archived commit
	commit := Commit{
		CommitID:  "archived1",
		Timestamp: "2025-01-01T00:00:00Z",
		Model:     "test",
		Datapoints: []Datapoint{
			{Prompt: "p1", Response: "### r1\nstructured"},
		},
	}
	data, _ := json.MarshalIndent(commit, "", "  ")
	os.WriteFile(filepath.Join(trainedDir, "commit_archived1.json"), data, 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ollamaCreateStatus{Status: "success"})
	}))
	defer srv.Close()

	err := RunTrain(srv.URL, "base-model", true)
	if err != nil {
		t.Errorf("RunTrain useAll: %v", err)
	}
}

func TestRunTrain_CreateModelFails(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	commitsDir := filepath.Join(dir, DatasetRoot, "commits")
	refsPath := filepath.Join(dir, DatasetRoot, "refs", "heads", "master")
	os.MkdirAll(commitsDir, 0755)
	os.MkdirAll(filepath.Dir(refsPath), 0755)

	commit := Commit{
		CommitID:  "test1",
		Timestamp: "2025-01-01T00:00:00Z",
		Model:     "test",
		Datapoints: []Datapoint{
			{Prompt: "p1", Response: "### r1\nok"},
		},
	}
	data, _ := json.MarshalIndent(commit, "", "  ")
	os.WriteFile(filepath.Join(commitsDir, "commit_test1.json"), data, 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ollamaCreateStatus{Error: "model creation failed"})
	}))
	defer srv.Close()

	err := RunTrain(srv.URL, "base-model", false)
	if err == nil {
		t.Error("expected error when model creation fails")
	}
}

func TestRunTrain_CommitsNoDatapoints(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	commitsDir := filepath.Join(dir, DatasetRoot, "commits")
	refsPath := filepath.Join(dir, DatasetRoot, "refs", "heads", "master")
	os.MkdirAll(commitsDir, 0755)
	os.MkdirAll(filepath.Dir(refsPath), 0755)

	commit := Commit{
		CommitID:   "empty1",
		Timestamp:  "2025-01-01T00:00:00Z",
		Model:      "test",
		Datapoints: []Datapoint{},
	}
	data, _ := json.MarshalIndent(commit, "", "  ")
	os.WriteFile(filepath.Join(commitsDir, "commit_empty1.json"), data, 0644)

	err := RunTrain("http://localhost:1", "base", false)
	if err != nil {
		t.Errorf("commits with no datapoints should return nil, got: %v", err)
	}
}

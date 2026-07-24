package dataset

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateOllamaModel_NonJSONResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	err := createOllamaModel(srv.URL, "test", "FROM base")
	if err != nil {
		t.Errorf("expected nil error for non-JSON 200, got: %v", err)
	}
}

func TestWriteCommit_JSONMarshalError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commitsDir := filepath.Join(dir, "commits")
	os.MkdirAll(commitsDir, 0755)
	refsPath := filepath.Join(dir, "refs.json")

	dps := []Datapoint{{Prompt: "p", Response: "r"}}
	_, err := writeCommit(commitsDir, refsPath, "model", dps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify commit file exists
	entries, _ := os.ReadDir(commitsDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 commit file, got %d", len(entries))
	}
}

func TestWriteCommit_ExistingRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commitsDir := filepath.Join(dir, "commits")
	os.MkdirAll(commitsDir, 0755)
	refsPath := filepath.Join(dir, "refs.json")

	// Write initial ref
	ref := RefPointer{LatestCommit: "first", TotalCommits: 1}
	data, _ := json.Marshal(ref)
	os.WriteFile(refsPath, data, 0644)

	dps := []Datapoint{{Prompt: "p", Response: "r"}}
	_, err := writeCommit(commitsDir, refsPath, "model", dps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify total commits incremented
	raw, _ := os.ReadFile(refsPath)
	var got RefPointer
	json.Unmarshal(raw, &got)
	if got.TotalCommits != 2 {
		t.Errorf("expected TotalCommits=2, got %d", got.TotalCommits)
	}
}

func TestRunInit_CommitsDirExists(t *testing.T) {
	t.Parallel()
	orig, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	defer os.Chdir(orig)

	// Create commits dir first
	os.MkdirAll(filepath.Join("data", "datasets", "commits"), 0755)
	os.MkdirAll(filepath.Join("data", "datasets", "refs", "heads"), 0755)

	err := RunInit()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsLowContent_WithStructure(t *testing.T) {
	t.Parallel()
	// has courtesy but also has ### → not low content
	resp := "Thank you for asking. ### Heading\nSome content."
	if isLowContent(resp) {
		t.Error("expected false for response with structure")
	}
}

func TestIsDegenerateRepeat_SingleElement(t *testing.T) {
	t.Parallel()
	dps := []Datapoint{{Prompt: "p", Response: "r"}}
	if isDegenerateRepeat(dps) {
		t.Error("expected false for single element")
	}
}

func TestIsDegenerateRepeat_Identical(t *testing.T) {
	t.Parallel()
	dps := []Datapoint{
		{Prompt: "p1", Response: "same response"},
		{Prompt: "p2", Response: "same response"},
	}
	if !isDegenerateRepeat(dps) {
		t.Error("expected true for identical responses")
	}
}

func TestIsDegenerateRepeat_LowContent(t *testing.T) {
	t.Parallel()
	dps := []Datapoint{
		{Prompt: "p1", Response: "Thank you for your question"},
		{Prompt: "p2", Response: "I appreciate your help"},
	}
	if !isDegenerateRepeat(dps) {
		t.Error("expected true for low-content courtesy pair")
	}
}

func TestIsDegenerateRepeat_Different(t *testing.T) {
	t.Parallel()
	dps := []Datapoint{
		{Prompt: "p1", Response: "different response one"},
		{Prompt: "p2", Response: "completely different response two"},
	}
	if isDegenerateRepeat(dps) {
		t.Error("expected false for different responses")
	}
}

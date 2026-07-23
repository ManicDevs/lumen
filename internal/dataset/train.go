package dataset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// trainedModelName is the name of the custom model produced by RunTrain.
const trainedModelName = "lumen-tuned"

// maxTrainMessages limits the number of messages used when training from
// fresh (unarchived) commits alone.
const maxTrainMessages = 200

type ollamaCreateRequest struct {
	Model     string `json:"model"`
	Modelfile string `json:"modelfile"`
	Stream    bool   `json:"stream"`
}

type ollamaCreateStatus struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func buildModelfile(baseModel string, messages []Datapoint) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s\n", baseModel)
	fmt.Fprintf(&b, "SYSTEM %s\n", quoteModelfileString(SystemPrompt))
	for _, m := range messages {
		fmt.Fprintf(&b, "MESSAGE user %s\n", quoteModelfileString(m.Prompt))
		fmt.Fprintf(&b, "MESSAGE assistant %s\n", quoteModelfileString(m.Response))
	}
	return b.String()
}

func quoteModelfileString(s string) string {
	s = strings.ReplaceAll(s, `"""`, `\"\"\"`)
	return `"""` + s + `"""`
}

func createOllamaModel(host, name, modelfile string) error {
	payload, err := json.Marshal(ollamaCreateRequest{Model: name, Modelfile: modelfile, Stream: false})
	if err != nil {
		return err
	}
	url := strings.TrimRight(host, "/") + "/api/create"
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("network error (is Ollama running at %s?): %w", host, err)
	}
	defer resp.Body.Close()

	var status ollamaCreateStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("ollama create failed (HTTP %d)", resp.StatusCode)
		}
		return nil
	}
	if status.Error != "" {
		return fmt.Errorf("ollama: %s", status.Error)
	}
	return nil
}

// RunTrain creates a custom Ollama model (lumen-tuned) from collected
// dataset commits. When useAll is false only fresh (unarchived) commits are
// used; when true all commits including previously trained ones are included.
func RunTrain(host, baseModel string, useAll bool) error {
	commitsDir := filepath.Join(DatasetRoot, "commits")
	trainedDir := filepath.Join(commitsDir, "trained")

	freshPaths, err := filepath.Glob(filepath.Join(commitsDir, "commit_*.json"))
	if err != nil {
		return fmt.Errorf("trainer: listing commits: %w", err)
	}

	allPaths := freshPaths
	if useAll {
		archivedPaths, err := filepath.Glob(filepath.Join(trainedDir, "commit_*.json"))
		if err != nil {
			return fmt.Errorf("trainer: listing archived commits: %w", err)
		}
		allPaths = append(append([]string{}, freshPaths...), archivedPaths...)
	}

	if len(allPaths) == 0 {
		fmt.Println("[Trainer] No commits found to train on yet.")
		return nil
	}

	var messages []Datapoint
	for _, p := range allPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var commit Commit
		if err := json.Unmarshal(data, &commit); err != nil {
			continue
		}
		messages = append(messages, commit.Datapoints...)
	}
	if len(messages) == 0 {
		fmt.Println("[Trainer] Commits found but none contained usable datapoints.")
		return nil
	}

	if !useAll && len(messages) > maxTrainMessages {
		messages = messages[len(messages)-maxTrainMessages:]
	}

	if useAll {
		fmt.Printf("[Trainer] Building %s from ALL %d commit(s) (fresh + archived), %d frame(s), no cap...\n", trainedModelName, len(allPaths), len(messages))
	} else {
		fmt.Printf("[Trainer] Building %s from %d fresh commit(s), %d frame(s)...\n", trainedModelName, len(freshPaths), len(messages))
	}

	modelfile := buildModelfile(baseModel, messages)

	if err := createOllamaModel(host, trainedModelName, modelfile); err != nil {
		return fmt.Errorf("trainer: creating model: %w", err)
	}
	fmt.Printf("[Trainer] Local model %q customized successfully.\n", trainedModelName)

	if err := os.MkdirAll(trainedDir, 0755); err != nil {
		return fmt.Errorf("trainer: creating trained archive dir: %w", err)
	}
	for _, p := range freshPaths {
		dest := filepath.Join(trainedDir, filepath.Base(p))
		if err := os.Rename(p, dest); err != nil {
			fmt.Printf("[!] Could not archive %s: %v\n", p, err)
		}
	}
	fmt.Printf("[Trainer] Archived %d processed commit(s) to %s\n", len(freshPaths), trainedDir)

	return nil
}

// Package dataset manages a lightweight, file-based dataset repository for
// collecting and curating LLM interaction frames from self-play generation
// sessions. Each session produces a "commit" of datapoints stored as JSON
// under data/datasets/commits/, with a ref pointer tracking the latest
// commit. The package also exposes RunTrain to produce a customised Ollama
// model from collected commits.
package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/llm"
	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

var courtesyPhrases = []string{
	"thank you for", "you're welcome", "i appreciate", "i'm glad",
	"feel free to ask", "i'm here to help", "keep up the good work",
	"glad i could help",
}

func isLowContent(resp string) bool {
	lower := strings.ToLower(resp)
	hasCourtesy := false
	for _, p := range courtesyPhrases {
		if strings.Contains(lower, p) {
			hasCourtesy = true
			break
		}
	}
	return hasCourtesy && !strings.Contains(resp, "###")
}

func isDegenerateRepeat(datapoints []Datapoint) bool {
	if len(datapoints) < 2 {
		return false
	}
	norm := func(s string) string {
		return strings.Join(strings.Fields(s), " ")
	}
	last := datapoints[len(datapoints)-1].Response
	prev := datapoints[len(datapoints)-2].Response
	if norm(last) != "" && norm(last) == norm(prev) {
		return true
	}
	return isLowContent(last) && isLowContent(prev)
}

func writeCommit(commitsDir, refsPath, model string, datapoints []Datapoint) (string, error) {
	canonical, err := json.Marshal(datapoints)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	commitID := fmt.Sprintf("%x", sum)

	now := time.Now().Format(time.RFC3339)
	commit := Commit{
		CommitID:   commitID,
		Timestamp:  now,
		Model:      model,
		Datapoints: datapoints,
	}
	commitData, err := json.MarshalIndent(commit, "", "  ")
	if err != nil {
		return "", err
	}
	commitPath := filepath.Join(commitsDir, "commit_"+commitID+".json")
	if err := os.WriteFile(commitPath, commitData, 0644); err != nil {
		return "", err
	}

	total := 1
	if existing, err := os.ReadFile(refsPath); err == nil {
		var ref RefPointer
		if json.Unmarshal(existing, &ref) == nil {
			total = ref.TotalCommits + 1
		}
	}
	ref := RefPointer{LatestCommit: commitID, LastUpdated: now, TotalCommits: total}
	refData, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(refsPath, refData, 0644); err != nil {
		return "", err
	}
	return commitID, nil
}

// RunGenerate starts a self-play generation session. When cont is true the
// model's own output is fed back as the next prompt (continuous chaining). If
// pipe is true, each completed frame set is committed to the dataset
// repository.
func RunGenerate(model, host string, cont, pipe bool, topic string) error {
	if topic == "" {
		topic = DefaultSeedTopics[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(DefaultSeedTopics))]
	}

	commitsDir := filepath.Join(DatasetRoot, "commits")
	stageDir := filepath.Join(DatasetRoot, "stage")
	refsPath := filepath.Join(DatasetRoot, "refs", "heads", "master")
	stagePath := filepath.Join(stageDir, "current.json")

	if pipe {
		if err := os.MkdirAll(commitsDir, 0755); err != nil {
			return fmt.Errorf("generate: creating commits dir: %w", err)
		}
		if err := os.MkdirAll(stageDir, 0755); err != nil {
			return fmt.Errorf("generate: creating stage dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(refsPath), 0755); err != nil {
			return fmt.Errorf("generate: creating refs dir: %w", err)
		}
	}

	eng := llm.NewLocalEngine(host, model, SystemPrompt, 8192, 60*time.Second, retry.Config{}, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n[!] Interrupted. Finalizing commit...")
		cancel()
	}()
	defer signal.Stop(sigChan)

	fmt.Println("[LUMEN SELF-PLAY DATA GENERATION]")
	if cont {
		fmt.Println("Running continuous self-chained generation... Press Ctrl+C to stop.")
	} else {
		fmt.Println("Running single-pass self-chained generation...")
	}
	if pipe {
		fmt.Printf("Committing collected frames under: %s\n", commitsDir)
	}
	fmt.Println("----------------------------------------------------------------")

	var datapoints []Datapoint
	currentPrompt := topic
	started := time.Now()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
		}

		fmt.Printf("[prompt]: %s\n", currentPrompt)

		fmt.Print("[response]: ")
		reply, err := eng.Send(ctx, []llm.ChatMessage{{Role: "user", Content: currentPrompt}}, func(token string) {
			fmt.Print(token)
		})
		fmt.Println()

		if reply != "" {
			datapoints = append(datapoints, Datapoint{Prompt: currentPrompt, Response: reply})
			if pipe {
				if data, jsonErr := json.MarshalIndent(datapoints, "", "  "); jsonErr == nil {
					_ = os.WriteFile(stagePath, data, 0644)
				}
			}
		}

		if err != nil {
			fmt.Printf("[!] Model call failed, stopping run: %v\n", err)
			break loop
		}

		currentPrompt = reply

		if !cont {
			break loop
		}

		if isDegenerateRepeat(datapoints) {
			fmt.Println("[!] Self-chain has collapsed into a non-generative loop (repeated or pure courtesy filler) — stopping run.")
			break loop
		}
	}

	var totalChars int
	for _, dp := range datapoints {
		totalChars += len(dp.Response)
	}
	fmt.Println("----------------------------------------------------------------")
	fmt.Printf("[RUN CONCLUDED] %d datapoint(s), %s elapsed, %d response chars generated\n",
		len(datapoints), time.Since(started).Round(time.Second), totalChars)

	if !pipe || len(datapoints) == 0 {
		return nil
	}

	commitID, err := writeCommit(commitsDir, refsPath, model, datapoints)
	if err != nil {
		return fmt.Errorf("generate: finalizing commit: %w", err)
	}
	_ = os.Remove(stagePath)
	fmt.Printf("Committed %d frame(s) as %s\n", len(datapoints), commitID)

	return nil
}

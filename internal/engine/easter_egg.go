package engine

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

	"gitlab.torproject.org/cerberus-droid/lumen/internal/retry"
)

// easterEggSystemPrompt scopes the local model strictly to system-design /
// performance-evaluation territory. This is inferred from real refusal text
// captured in commit_2598618...json ("doesn't relate to evaluating system
// designs or identifying bottlenecks"), so wording here is chosen to be
// consistent with that observed behavior rather than guessed from scratch.
const easterEggSystemPrompt = "You only answer questions related to evaluating system designs, " +
	"identifying performance bottlenecks, or optimizing solutions within strict engineering " +
	"constraints. If a request doesn't relate to evaluating system designs or identifying " +
	"bottlenecks, politely decline and steer the conversation back to system architecture, " +
	"performance optimization, or scalability topics."

// defaultSeedTopics mirrors the flavor of prompts actually observed in the
// real commit artifacts (spatial-indexing DB clustering, eBPF vs iptables,
// etc.) — all system-design / performance-evaluation prompts, never content
// pulled from a user's own project.
var defaultSeedTopics = []string{
	"Propose a spatial-indexing database clustering architecture optimized to aggregate real-time geolocation telemetry from 12 million concurrent IoT devices.",
	"Analyze performance impacts of eBPF socket filters versus kernel iptables rules.",
	"Evaluate the bottlenecks in a synchronous request/response microservice mesh under a 50x traffic spike.",
	"Design a write-heavy time-series ingestion pipeline that must sustain 2 million writes per second with sub-100ms read-after-write consistency.",
}

// datasetRoot is the base directory for the versioned synthetic dataset.
const datasetRoot = "data/datasets"

// Datapoint is one self-chained exchange: prompt in, model response out.
// Field names are lowercase to match the real, on-disk commit schema —
// consumed downstream by RunLocalTrain in trainer.go.
type Datapoint struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

// Commit is one finalized run of the self-play loop, content-addressed by
// commit_id and written to data/datasets/commits/commit_<id>.json.
type Commit struct {
	CommitID   string      `json:"commit_id"`
	Timestamp  string      `json:"timestamp"`
	Model      string      `json:"model"`
	Datapoints []Datapoint `json:"datapoints"`
}

// refPointer mirrors data/datasets/refs/heads/master.
type refPointer struct {
	LatestCommit string `json:"latest_commit"`
	LastUpdated  string `json:"last_updated"`
	TotalCommits int    `json:"total_commits"`
}

// RunBunnyEasterEgg runs the local self-play synthetic-data generator.
//
// Each iteration feeds the model's previous response back in as the next
// prompt (a single self-chained conversation, not two distinct proposer/
// critic roles — confirmed by the varying, non-paired datapoint counts
// seen across the real commit files). Single-shot does exactly one
// exchange; --continuous loops until interrupted. On a clean finish
// (natural end or Ctrl+C), if --pipe-dataset was passed, the collected
// datapoints are hashed and written as one commit file, and
// refs/heads/master is advanced to point at it.
func RunBunnyEasterEgg(model, host string, cont, pipe bool, topic string) error {
	if topic == "" {
		topic = defaultSeedTopics[rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(defaultSeedTopics))]
	}

	commitsDir := filepath.Join(datasetRoot, "commits")
	stageDir := filepath.Join(datasetRoot, "stage")
	refsPath := filepath.Join(datasetRoot, "refs", "heads", "master")
	stagePath := filepath.Join(stageDir, "current.json")

	if pipe {
		if err := os.MkdirAll(commitsDir, 0755); err != nil {
			return fmt.Errorf("easter egg: creating commits dir: %w", err)
		}
		if err := os.MkdirAll(stageDir, 0755); err != nil {
			return fmt.Errorf("easter egg: creating stage dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(refsPath), 0755); err != nil {
			return fmt.Errorf("easter egg: creating refs dir: %w", err)
		}
	}

	eng := NewLocalEngine(BackendOllama, host, model, easterEggSystemPrompt, 8192, 60*time.Second, retry.Config{}, slog.Default())

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
		reply, err := eng.Send(ctx, []ChatMessage{{Role: "user", Content: currentPrompt}}, func(token string) {
			fmt.Print(token)
		})
		fmt.Println()

		// A Ctrl+C mid-generation still returns whatever text had already
		// streamed to the terminal above; keep it as the final datapoint
		// rather than throwing away tokens the model (and the user) already
		// saw, then stop.
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
		return fmt.Errorf("easter egg: finalizing commit: %w", err)
	}
	_ = os.Remove(stagePath)
	fmt.Printf("Committed %d frame(s) as %s\n", len(datapoints), commitID)

	return nil
}

// courtesyPhrases are stock conversational-filler openers/closers that show
// up once a self-chained exchange stops exchanging technical content and
// starts just thanking itself in a loop (observed directly in production
// runs: turns settle into "Thank you for..." / "You're welcome!" / "feel
// free to ask" with no new substance).
var courtesyPhrases = []string{
	"thank you for", "you're welcome", "i appreciate", "i'm glad",
	"feel free to ask", "i'm here to help", "keep up the good work",
	"glad i could help",
}

// isLowContent reports whether a response looks like pure courtesy filler:
// it contains a stock acknowledgment phrase and has no markdown heading,
// which in practice is what distinguishes "thanks, will do!" turns from
// turns that are still restating structured technical content (even if
// that content is itself circular, a heading means the model is still
// producing a structured answer rather than just being polite).
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

// isDegenerateRepeat reports whether the self-chain has collapsed into a
// non-generative loop: either the last two responses are literally
// identical, or the last two are both low-content courtesy filler (see
// isLowContent). Plain word-overlap similarity was tried first and
// rejected — same-topic technical turns share enough vocabulary that it
// couldn't separate real elaboration from a "thank you" spiral; the
// courtesy+no-heading signal does.
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

// writeCommit hashes the datapoints into a content-addressed commit id,
// writes commits/commit_<id>.json, and advances refs/heads/master.
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
		var ref refPointer
		if json.Unmarshal(existing, &ref) == nil {
			total = ref.TotalCommits + 1
		}
	}
	ref := refPointer{LatestCommit: commitID, LastUpdated: now, TotalCommits: total}
	refData, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(refsPath, refData, 0644); err != nil {
		return "", err
	}

	return commitID, nil
}

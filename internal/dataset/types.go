package dataset

// DatasetRoot is the default root directory for the dataset repository,
// relative to the working directory.
const DatasetRoot = "data/datasets"

// SystemPrompt is the system prompt used during self-play generation. It
// constrains the model to discuss system architecture, performance topics, and
// code review scenarios.

const SystemPrompt = "You are a senior software engineer reviewing source code for bugs, " +
	"performance issues, security vulnerabilities, and correctness problems. " +
	"Analyze the provided code directly and specifically for logic errors, edge cases, " +
	"security issues, and performance anti-patterns, citing exact function and variable names. " +
	"Also discuss system architecture, performance optimization, and scalability topics when relevant. " +
	"Always respond in Markdown with code in fenced blocks."

// DefaultSeedTopics is a list of seed topics used when no topic is specified
// during self-play generation.
var DefaultSeedTopics = []string{
	"Propose a spatial-indexing database clustering architecture optimized to aggregate real-time geolocation telemetry from 12 million concurrent IoT devices.",
	"Analyze performance impacts of eBPF socket filters versus kernel iptables rules.",
	"Evaluate the bottlenecks in a synchronous request/response microservice mesh under a 50x traffic spike.",
	"Design a write-heavy time-series ingestion pipeline that must sustain 2 million writes per second with sub-100ms read-after-write consistency.",
	"Review this Go code for potential nil pointer dereferences, race conditions, and memory leaks.",
	"Identify SQL injection vulnerabilities and insecure cryptographic patterns in this codebase.",
	"Find performance anti-patterns: N+1 queries, unnecessary allocations, lock contention in hot paths.",
	"Detect missing error handling, improper resource cleanup, and goroutine leaks.",
	"Flag hardcoded secrets, weak randomness, and insecure defaults in configuration.",
	"Spot architectural issues: tight coupling, violation of separation of concerns, circular dependencies.",
}

// Datapoint is a single prompt-response pair collected during self-play.
type Datapoint struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

// Commit is a named snapshot of one or more Datapoints stored as a JSON file.
type Commit struct {
	CommitID   string      `json:"commit_id"`
	Timestamp  string      `json:"timestamp"`
	Model      string      `json:"model"`
	Datapoints []Datapoint `json:"datapoints"`
}

// RefPointer tracks the latest commit on a branch in the dataset repository.
type RefPointer struct {
	LatestCommit string `json:"latest_commit"`
	LastUpdated  string `json:"last_updated"`
	TotalCommits int    `json:"total_commits"`
}

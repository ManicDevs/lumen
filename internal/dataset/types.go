package dataset

// DatasetRoot is the default root directory for the dataset repository,
// relative to the working directory.
const DatasetRoot = "data/datasets"

// SystemPrompt is the system prompt used during self-play generation. It
// constrains the model to discuss system architecture and performance topics.

const SystemPrompt = "You only answer questions related to evaluating system designs, " +
	"identifying performance bottlenecks, or optimizing solutions within strict engineering " +
	"constraints. If a request doesn't relate to evaluating system designs or identifying " +
	"bottlenecks, politely decline and steer the conversation back to system architecture, " +
	"performance optimization, or scalability topics."

var DefaultSeedTopics = []string{
	"Propose a spatial-indexing database clustering architecture optimized to aggregate real-time geolocation telemetry from 12 million concurrent IoT devices.",
	"Analyze performance impacts of eBPF socket filters versus kernel iptables rules.",
	"Evaluate the bottlenecks in a synchronous request/response microservice mesh under a 50x traffic spike.",
	"Design a write-heavy time-series ingestion pipeline that must sustain 2 million writes per second with sub-100ms read-after-write consistency.",
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

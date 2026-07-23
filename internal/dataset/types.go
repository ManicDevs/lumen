package dataset

const DatasetRoot = "data/datasets"

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

type Datapoint struct {
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

type Commit struct {
	CommitID   string      `json:"commit_id"`
	Timestamp  string      `json:"timestamp"`
	Model      string      `json:"model"`
	Datapoints []Datapoint `json:"datapoints"`
}

type RefPointer struct {
	LatestCommit string `json:"latest_commit"`
	LastUpdated  string `json:"last_updated"`
	TotalCommits int    `json:"total_commits"`
}

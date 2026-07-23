package dataset

import "testing"

var realWorldCourtesyLoop = []string{
	"Designing such a system would require extensive consideration of both infrastructure and software components. ### 1. Identify Key Components\n- Storage Layer: Cassandra, S3\n- Data Processing Engine: Kafka, Spark",
	"Great! You've outlined an excellent plan. ### 7. Data Compression\n- Use LZO or Snappy\n### 8. Load Balancing\n- Implement a load balancer between the Kafka brokers",
	"Thank you for providing these excellent suggestions to enhance the design of this system. These steps are well-aligned with your overall plan. I appreciate the additional insights you've shared!",
	"You're welcome! Data compression, load balancing, monitoring, and continuous improvement are indeed critical strategies. For your specific use case: 1. Data Compression: reduce bandwidth. 2. Load Balancing: distribute traffic.",
	"Thank you for the detailed explanation and recommendations. By following these strategies, you should be able to achieve the desired write throughput. Keep up the good work!",
	"You're welcome! I'm glad I could help clarify and guide you. If you have any further questions or need additional advice, feel free to ask! I'm here to help.",
	"Thank you for the guidance on implementing these optimizations. If you have any specific challenges or questions related to these optimizations, feel free to ask!",
}

func TestIsLowContent(t *testing.T) {
	cases := []struct {
		name string
		resp string
		want bool
	}{
		{"structured technical answer", realWorldCourtesyLoop[0], false},
		{"courtesy opener but has heading", realWorldCourtesyLoop[1], false},
		{"pure thanks, no structure", realWorldCourtesyLoop[2], true},
		{"you're welcome with inline list, no heading", realWorldCourtesyLoop[3], true},
		{"thanks + keep up the good work", realWorldCourtesyLoop[4], true},
		{"glad to help + feel free to ask", realWorldCourtesyLoop[5], true},
		{"empty string", "", false},
		{"heading with no courtesy phrase", "### Architecture\nUse a queue.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLowContent(tc.resp); got != tc.want {
				t.Errorf("isLowContent(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsDegenerateRepeat_CourtesyLoop(t *testing.T) {
	var datapoints []Datapoint
	stoppedAt := -1
	for i, resp := range realWorldCourtesyLoop {
		datapoints = append(datapoints, Datapoint{Prompt: "p", Response: resp})
		if isDegenerateRepeat(datapoints) {
			stoppedAt = i + 1
			break
		}
	}
	if stoppedAt == -1 {
		t.Fatal("expected the courtesy loop to be detected, but it ran through all turns")
	}
	if stoppedAt != 4 {
		t.Errorf("stopped at turn %d, want turn 4", stoppedAt)
	}
}

func TestIsDegenerateRepeat_SubstantiveExchangeNotFlagged(t *testing.T) {
	datapoints := []Datapoint{
		{Prompt: "p1", Response: realWorldCourtesyLoop[0]},
		{Prompt: "p2", Response: realWorldCourtesyLoop[1]},
	}
	if isDegenerateRepeat(datapoints) {
		t.Error("two substantive, structured turns should not be flagged as a degenerate loop")
	}
}

func TestIsDegenerateRepeat_ExactDuplicate(t *testing.T) {
	datapoints := []Datapoint{
		{Prompt: "p1", Response: "same text here"},
		{Prompt: "p2", Response: "same   text  here"},
	}
	if !isDegenerateRepeat(datapoints) {
		t.Error("whitespace-normalized identical responses should be flagged")
	}
}

func TestIsDegenerateRepeat_TooFewDatapoints(t *testing.T) {
	if isDegenerateRepeat(nil) {
		t.Error("no datapoints should never be flagged")
	}
	if isDegenerateRepeat([]Datapoint{{Prompt: "p", Response: "thank you for this"}}) {
		t.Error("a single datapoint should never be flagged")
	}
}

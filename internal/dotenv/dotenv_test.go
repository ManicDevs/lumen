package dotenv

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	input := `
# a comment
CLOUD_API_KEY=abc123
QUOTED="hello world"
SINGLE_QUOTED='foo bar'

MALFORMED_LINE_NO_EQUALS
  SPACED_KEY = spaced value
EMPTY_VAL=
`
	got, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"CLOUD_API_KEY": "abc123",
		"QUOTED":        "hello world",
		"SINGLE_QUOTED": "foo bar",
		"SPACED_KEY":    "spaced value",
		"EMPTY_VAL":     "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["MALFORMED_LINE_NO_EQUALS"]; ok {
		t.Errorf("malformed line should not produce a key")
	}
}

func TestApplyToEnviron_RealValueWins(t *testing.T) {
	existing := map[string]string{"CLOUD_API_KEY": "from_shell"}
	parsed := map[string]string{"CLOUD_API_KEY": "from_dotenv"}
	merged := ApplyToEnviron(existing, parsed)
	if merged["CLOUD_API_KEY"] != "from_shell" {
		t.Errorf("expected real env value to win, got %q", merged["CLOUD_API_KEY"])
	}
}

func TestApplyToEnviron_EmptyExportedVarFallsThrough(t *testing.T) {
	// Regression test for the bug found in production use: a variable
	// exported with an empty value (e.g. `export CLOUD_API_KEY=`) must be
	// treated the same as unset, so .env can still supply the real value.
	existing := map[string]string{"CLOUD_API_KEY": ""}
	parsed := map[string]string{"CLOUD_API_KEY": "from_dotenv"}
	merged := ApplyToEnviron(existing, parsed)
	if merged["CLOUD_API_KEY"] != "from_dotenv" {
		t.Errorf("expected empty-but-exported var to fall through to .env value, got %q", merged["CLOUD_API_KEY"])
	}
}

func TestApplyToEnviron_TrulyUnsetFallsThrough(t *testing.T) {
	existing := map[string]string{}
	parsed := map[string]string{"CLOUD_API_KEY": "from_dotenv"}
	merged := ApplyToEnviron(existing, parsed)
	if merged["CLOUD_API_KEY"] != "from_dotenv" {
		t.Errorf("expected unset var to pick up .env value, got %q", merged["CLOUD_API_KEY"])
	}
}

func TestLoad_MissingFileIsNotError(t *testing.T) {
	got, err := Load("/nonexistent/path/.env")
	if err != nil {
		t.Fatalf("missing .env should not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for missing file, got %v", got)
	}
}

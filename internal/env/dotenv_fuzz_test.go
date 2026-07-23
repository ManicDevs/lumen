package env

import (
	"strings"
	"testing"
)

func FuzzParseDotenv(f *testing.F) {
	seeds := []string{
		"KEY=value\nOTHER=val",
		"# comment\nKEY=val\n",
		`QUOTED="hello world"`,
		`KEY=`,
		`=value`,
		`A=1\nB=2\nC=3`,
		`  SPACED  =  spaced  `,
		`KEY="unclosed`,
		`KEY='single'unmatched`,
		`MULTI_LINE="line1
line2"`,
		`KEY_WITH_#_HASH=test`,
		`EMPTY_QUOTED=""`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		result, err := ParseDotenv(strings.NewReader(input))
		if err != nil {
			t.Skip()
		}
		for k, v := range result {
			if k == "" {
				t.Errorf("empty key produced")
			}
			if strings.Contains(k, "=") {
				t.Errorf("key contains '=': %q", k)
			}
			if strings.TrimSpace(k) != k {
				t.Errorf("key has leading/trailing whitespace: %q", k)
			}
			_ = v
		}
	})
}

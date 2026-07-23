package output

import "testing"

func TestSecrets(t *testing.T) {
	in := "error calling https://api.example.com/v1?key=AQ.Ab8RN6ICUPTuMsp got 404"
	got := Secrets(in, "AQ.Ab8RN6ICUPTuMsp")
	if got == in {
		t.Fatal("expected secret to be redacted")
	}
	if containsStr(got, "AQ.Ab8RN6ICUPTuMsp") {
		t.Errorf("secret still present in redacted output: %q", got)
	}
}

func TestSecrets_EmptySecretIgnored(t *testing.T) {
	in := "no secrets here"
	got := Secrets(in, "")
	if got != in {
		t.Errorf("empty secret should not alter string, got %q", got)
	}
}

func TestURLQueryParam(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		param string
		want  string
	}{
		{
			name:  "key in middle",
			url:   "https://x.com/v1beta/models/foo:streamGenerateContent?alt=sse&key=SECRET123&other=1",
			param: "key",
			want:  "https://x.com/v1beta/models/foo:streamGenerateContent?alt=sse&key=[REDACTED]&other=1",
		},
		{
			name:  "key at end",
			url:   "https://x.com/v1?key=SECRET123",
			param: "key",
			want:  "https://x.com/v1?key=[REDACTED]",
		},
		{
			name:  "param absent",
			url:   "https://x.com/v1?other=1",
			param: "key",
			want:  "https://x.com/v1?other=1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := URLQueryParam(c.url, c.param)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

package version

import (
	"testing"
)

func TestVersion_NonEmpty(t *testing.T) {
	t.Parallel()
	if Version == "" {
		t.Error("Version should not be empty")
	}
	if Commit == "" {
		t.Error("Commit should not be empty")
	}
	if Date == "" {
		t.Error("Date should not be empty")
	}
}

func TestVersion_String(t *testing.T) {
	t.Parallel()
	s := String()
	if s == "" {
		t.Error("String() should not return empty string")
	}
	if !containsStr(s, Version) {
		t.Errorf("String() should contain Version, got: %s", s)
	}
	if !containsStr(s, Commit) {
		t.Errorf("String() should contain Commit, got: %s", s)
	}
	if !containsStr(s, Date) {
		t.Errorf("String() should contain Date, got: %s", s)
	}
	if !containsStr(s, GoVersion) {
		t.Errorf("String() should contain GoVersion, got: %s", s)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || len(s) > 0 && (s[0:len(substr)] == substr || containsStr(s[1:], substr)))
}
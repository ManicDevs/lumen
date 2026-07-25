package prompt

import (
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	t.Parallel()
	tmpl, ok := Get("code-review")
	if !ok {
		t.Fatal("expected code-review template to exist")
	}
	if tmpl.Name != "Code Review" {
		t.Errorf("expected name 'Code Review', got %q", tmpl.Name)
	}
}

func TestGetUnknown(t *testing.T) {
	t.Parallel()
	_, ok := Get("nonexistent")
	if ok {
		t.Fatal("expected false for unknown template")
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	list := List()
	if len(list) < 5 {
		t.Errorf("expected at least 5 templates, got %d", len(list))
	}
	names := make(map[string]bool)
	for _, l := range list {
		names[l.Name] = true
	}
	for _, want := range []string{"Code Review", "Security Review", "Performance Review", "SQL Injection Review", "Concurrency Review"} {
		if !names[want] {
			t.Errorf("missing template %q", want)
		}
	}
}

func TestRender(t *testing.T) {
	t.Parallel()
	tmpl := Template{User: "Review: {{code}}"}
	result := tmpl.Render("func main() {}")
	if result != "Review: func main() {}" {
		t.Errorf("unexpected render: %q", result)
	}
}

func TestRenderEmpty(t *testing.T) {
	t.Parallel()
	tmpl := Template{User: "no placeholder"}
	result := tmpl.Render("code")
	if result != "no placeholder" {
		t.Errorf("expected no change, got %q", result)
	}
}

func TestAllTemplatesHaveFields(t *testing.T) {
	t.Parallel()
	for _, tmpl := range List() {
		if tmpl.Name == "" {
			t.Error("template with empty name")
		}
		if tmpl.System == "" {
			t.Errorf("template %q has empty System", tmpl.Name)
		}
		if tmpl.User == "" {
			t.Errorf("template %q has empty User", tmpl.Name)
		}
		if !strings.Contains(tmpl.User, "{{code}}") {
			t.Errorf("template %q User missing {{code}} placeholder", tmpl.Name)
		}
	}
}

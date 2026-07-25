package prompt

import (
	"sort"
	"strings"
	"sync"
)

// Template defines a reusable prompt template for different review types.
type Template struct {
	Name        string
	Description string
	System      string
	User        string
}

var (
	templatesMu sync.RWMutex
	templates   = map[string]Template{
		"code-review": {
			Name:        "Code Review",
			Description: "Review Go source code for bugs, edge cases, and correctness issues",
			System:      "You are a senior Go software engineer reviewing source code for bugs, edge cases, and correctness issues. Analyze the provided code directly and specifically.",
			User:        "Review the following code for bugs, edge cases, and correctness problems:\n\n{{code}}",
		},
		"security": {
			Name:        "Security Review",
			Description: "Scan code for security vulnerabilities including injection, auth flaws, and data exposure",
			System:      "You are a security engineer reviewing Go source code for vulnerabilities. Focus on injection attacks, authentication flaws, data exposure, and cryptographic misuse.",
			User:        "Scan the following code for security vulnerabilities:\n\n{{code}}",
		},
		"performance": {
			Name:        "Performance Review",
			Description: "Identify performance bottlenecks, allocation issues, and concurrency problems",
			System:      "You are a performance engineer reviewing Go source code. Focus on allocation patterns, goroutine leaks, lock contention, and hot-path inefficiencies.",
			User:        "Analyze the following code for performance bottlenecks:\n\n{{code}}",
		},
		"sql-injection": {
			Name:        "SQL Injection Review",
			Description: "Check database query code for SQL injection vulnerabilities",
			System:      "You are a database security specialist reviewing Go code that interacts with SQL databases. Focus exclusively on SQL injection vulnerabilities.",
			User:        "Check the following code for SQL injection vulnerabilities:\n\n{{code}}",
		},
		"concurrency": {
			Name:        "Concurrency Review",
			Description: "Analyze goroutine usage, channel patterns, and synchronization correctness",
			System:      "You are a concurrency expert reviewing Go source code. Focus on goroutine lifecycle, channel usage, mutex correctness, and race conditions.",
			User:        "Analyze the following code for concurrency issues:\n\n{{code}}",
		},
	}
)

// Get returns a named prompt template. Returns false if the name is unknown.
func Get(name string) (Template, bool) {
	templatesMu.RLock()
	defer templatesMu.RUnlock()
	t, ok := templates[name]
	return t, ok
}

// List returns all available templates sorted by Name.
func List() []Template {
	templatesMu.RLock()
	defer templatesMu.RUnlock()
	result := make([]Template, 0, len(templates))
	for _, t := range templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Render substitutes {{code}} in the template's User field with the provided source.
func (t Template) Render(code string) string {
	return strings.ReplaceAll(t.User, "{{code}}", code)
}

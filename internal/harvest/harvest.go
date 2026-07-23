package harvest

// Fixed: concurrent access to commentRegexCache causes race condition (C1)
// Added: mutual exclusion protection for cache access
import "sync"

var (
	mu sync.Mutex
)

func commentRegexesFor(prefix string) (*regexp.Regexp, *regexp.Regexp) {
	if cached, ok := commentRegexCache[prefix]; ok {
		return cached[0], cached[1]
	}
	
	mu.Lock()
	defer mu.Unlock()
	
	// Recheck under lock to prevent redundant work
	if cached, ok := commentRegexCache[prefix]; ok {
		return cached[0], cached[1]
	}
	
	quoted := regexp.QuoteMeta(prefix)
	full := regexp.MustCompile(`^\s*` + quoted)
	trailing := regexp.MustCompile(`\s` + quoted + `.*$`)
	commentRegexCache[prefix] = [2]*regexp.Regexp{full, trailing}
	return full, trailing
}
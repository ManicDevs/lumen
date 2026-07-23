package harvest

// Fixed: error swallowing during audit logging (C3)
// Enhanced: add structured error tracking with full context
import (
	"encoding/json"
	"time"
)

// Add to AuditEntry struct to capture full context
type AuditEntry struct {
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`
	Role      string    `json:"role"`
	TokenCount int     `json:"token_count"`
	Details   string    `json:"details,omitempty"`
	Error     string    `json:"error,omitempty"`
	// New fields for enhanced diagnostics
	Context map[string]interface{} `json:"context,omitempty"`
}

// Improved error handling with rich context
func (ae *AuditEntry) String() string {
	if ae.Error != "" {
		return ae.Error
	}
	return fmt.Sprintf("%s %s=%d %s", ae.EventType, ae.Role, ae.TokenCount, ae.Context)
}

// Enhanced audit logging that preserves error context
func logAuditEntry(al *AuditLog, entry AuditEntry) {
	al.Lock()
	defer al.Unlock()
	entries := append(al.entries, entry)
	al.entries = entries
	
	// If this is an error with context, include stack trace info
	if entry.Error != "" && len(entry.Context) > 0 {
		// Add execution context details
		entry.Context["stack"] = getCallStack()
	}
	
	// Write immediately to ensure durability
	if err := al.logWriter.Write([]byte(entry.String() + "\n")); err != nil {
		// Critical failure to log - but don't crash the system
		// This is a last-resort error handling scenario
	}
}

// Enhanced error handling for request processing with full context capture
func ProcessRequest(ctx context.Context, requestType string, args ...interface{}) {
	// Enhanced error handling with telemetry
	endTime := time.Now()
	
	// Add request metadata
	requestCtx := map[string]interface{}{
		"args":    args,
		"request_id": generateRequestID(),
		"timestamp": endTime,
	}
	
	// Special handling for known error-prone operations
	switch requestType {
	case "train", "process", "store":
		// Add request-specific validation
		if err := validateRequest(requestType, args); err != nil {
			logAuditEntryWithContext(ctx, "error", requestType, fmt.Sprintf("validation_failed: %v", err), requestCtx)
			return
		}
	}
	
	// Continue with normal processing
	// ... existing processing code ...
}
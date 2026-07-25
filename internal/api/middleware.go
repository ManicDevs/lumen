package api

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request-id"

// RequestIDFromContext extracts the request ID from the context.
// Returns an empty string if no request ID is present.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// withRequestID generates a unique request ID for each incoming request,
// sets it as the X-Request-ID response header, and stores it in the
// request context for downstream handlers.
func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := generateRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// generateRequestID creates a cryptographically random 32-character
// hex string suitable for use as a request identifier.
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("generating request ID: %w", err))
	}
	return hex.EncodeToString(b)
}

// withSecurityHeaders sets defensive security headers on every response.
// The Strict-Transport-Security header is only set when the request is
// served over TLS.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://unpkg.com; "+
				"style-src 'self' 'unsafe-inline' https://unpkg.com; "+
				"img-src 'self' data:")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security",
				"max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// gzipResponseWriter wraps http.ResponseWriter to support gzip compression.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	flushed bool
}

// Write appends data to the gzip writer and updates the internal size tracker.
func (gw *gzipResponseWriter) Write(b []byte) (int, error) {
	return gw.gz.Write(b)
}

// Flush flushes the gzip writer and then flushes the underlying response writer
// if it implements http.Flusher.
func (gw *gzipResponseWriter) Flush() {
	gw.gz.Flush()
	gw.flushed = true
	if f, ok := gw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying http.ResponseWriter for compatibility
// with http.ResponseController.
func (gw *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return gw.ResponseWriter
}

// withCompression applies gzip compression when the client accepts it.
// Responses with Content-Encoding already set are passed through uncompressed.
func (s *Server) withCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// If content is already compressed, skip.
		if w.Header().Get("Content-Encoding") != "" {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Vary", "Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")

		gz, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		gzw := &gzipResponseWriter{
			ResponseWriter: w,
			gz:             gz,
		}

		next.ServeHTTP(gzw, r)

		gz.Close()
	})
}

// bucket represents a token bucket for rate limiting.
type bucket struct {
	tokens   float64
	lastFill time.Time
	rate     float64
	burst    float64
	mu       sync.Mutex
}

// rateLimitState manages per-IP rate limiting with a token bucket algorithm.
type rateLimitState struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
}

// newRateLimitState creates a new rate limiter with the given rate and burst.
func newRateLimitState(rate, burst float64) *rateLimitState {
	return &rateLimitState{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
	}
}

// allow checks whether a request from the given IP should be permitted.
func (rl *rateLimitState) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	if !ok {
		b = &bucket{
			tokens:   rl.burst,
			lastFill: time.Now(),
			rate:     rl.rate,
			burst:    rl.burst,
		}
		rl.buckets[ip] = b
	}

	return b.allow()
}

// cleanup removes stale buckets that haven't been used recently.
func (rl *rateLimitState) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	for ip, b := range rl.buckets {
		b.mu.Lock()
		stale := b.lastFill.Before(cutoff)
		b.mu.Unlock()
		if stale {
			delete(rl.buckets, ip)
		}
	}
}

// allow refills tokens based on elapsed time and checks whether one
// token can be consumed.
func (b *bucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.lastFill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// withRateLimit enforces per-IP rate limiting using a token bucket algorithm.
// Returns 429 Too Many Requests with a Retry-After header when the limit
// is exceeded.
func (s *Server) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = strings.Split(fwd, ",")[0]
		}
		ip = strings.TrimSpace(ip)

		if !s.rateLimiter.allow(ip) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// withBodyLimit returns a middleware that limits the size of request bodies.
// Requests exceeding maxBytes receive a 413 Payload Too Large response.
func (s *Server) withBodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// withRecovery wraps a handler to recover from panics, log the stack trace,
// and return a 500 Internal Server Error.
func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic recovered",
					"err", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", getStack(),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// getStack returns a formatted stack trace string for panic logging.
func getStack() string {
	const size = 4096
	buf := make([]byte, size)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

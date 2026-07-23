package ollama_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/ollama"
)

// This example demonstrates creating a Client, calling Chat, and printing the
// response. It uses httptest so it is self-contained and runnable.
func ExampleClient_chat() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Hello from the test server!"}}`)
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	resp, err := c.Chat(context.Background(), ollama.ChatRequest{
		Model: "test-model",
		Messages: []ollama.Message{
			{Role: "user", Content: "Say hello"},
		},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(resp.Message.Content)
	// Output: Hello from the test server!
}

func ExampleClient_list() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"models":[{"name":"qwen2.5-coder:3b","size":1234567}]}`)
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	list, err := c.List(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, m := range list.Models {
		fmt.Println(m.Name)
	}
	// Output: qwen2.5-coder:3b
}

func ExampleClient_version() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"version":"0.31.1"}`)
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	v, err := c.Version(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(v)
	// Output: 0.31.1
}

func ExampleClient_health() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	err := c.Server().Health(context.Background())
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("ok")
	// Output: ok
}

func ExampleNewClient_customHTTP() {
	// A custom HTTP client with a short timeout.
	hc := &http.Client{Timeout: 0}
	c := ollama.NewClient("http://localhost:11434", ollama.WithHTTPClient(hc))
	fmt.Println(strings.Contains(c.BaseURL(), "localhost"))
	// Output: true
}

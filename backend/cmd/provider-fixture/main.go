// provider-fixture is a local-only compatibility fixture for exercising HAI's
// provider HTTP contracts. It is deliberately not an LLM and never accepts
// credentials, contacts external services, or returns model-generated text.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	defaultAddress = ":11434"
	ollamaModel    = "hai-fixture-ollama:latest"
	openAIModel    = "hai-fixture-openai"
)

func main() {
	address := flag.String("listen", defaultAddress, "HTTP listen address")
	healthcheck := flag.Bool("healthcheck", false, "check the local fixture health endpoint")
	flag.Parse()

	if *healthcheck {
		if err := checkHealth(*address); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	server := &http.Server{
		Addr:              *address,
		Handler:           newHandler(),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkHealth(address string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1" + address + "/healthz")
	if err != nil {
		return fmt.Errorf("provider fixture health request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("provider fixture health returned %s", response.Status)
	}
	return nil
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", staticJSON(http.StatusOK, map[string]any{"status": "ok"}))
	mux.HandleFunc("/api/tags", method(http.MethodGet, staticJSON(http.StatusOK, map[string]any{
		"models": []map[string]string{{"name": ollamaModel, "model": ollamaModel}},
	})))
	mux.HandleFunc("/v1/models", method(http.MethodGet, staticJSON(http.StatusOK, map[string]any{
		"object": "list",
		"data":   []map[string]string{{"id": openAIModel, "object": "model"}},
	})))
	mux.HandleFunc("/api/generate", method(http.MethodPost, staticJSON(http.StatusOK, map[string]any{
		"model": ollamaModel, "response": "HAI provider fixture response", "done": true,
	})))
	mux.HandleFunc("/v1/chat/completions", method(http.MethodPost, staticJSON(http.StatusOK, map[string]any{
		"id": "hai-provider-fixture", "object": "chat.completion", "model": openAIModel,
		"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": "HAI provider fixture response"}, "finish_reason": "stop"}},
	})))
	return mux
}

func method(expected string, next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != expected {
			writer.Header().Set("Allow", expected)
			staticJSON(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})(writer, request)
			return
		}
		next(writer, request)
	}
}

func staticJSON(status int, value any) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(value)
	}
}

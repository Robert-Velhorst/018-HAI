package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderFixtureSupportsOllamaAndOpenAIProbeContracts(t *testing.T) {
	server := httptest.NewServer(newHandler())
	defer server.Close()

	for _, test := range []struct {
		path      string
		modelPath []string
		modelID   string
	}{
		{path: "/api/tags", modelPath: []string{"models", "0", "name"}, modelID: ollamaModel},
		{path: "/v1/models", modelPath: []string{"data", "0", "id"}, modelID: openAIModel},
	} {
		response, err := http.Get(server.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Fatalf("GET %s status = %d", test.path, response.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		value := body[test.modelPath[0]].([]any)[0].(map[string]any)[test.modelPath[2]]
		if value != test.modelID {
			t.Fatalf("GET %s model = %q, want %q", test.path, value, test.modelID)
		}
	}
}

func TestProviderFixtureRejectsWrongMethod(t *testing.T) {
	response := httptest.NewRecorder()
	newHandler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/tags", nil))
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("response = %d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

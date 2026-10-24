package openclawmaintenance

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type healthResponseReadFailure struct{}

func (healthResponseReadFailure) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

type countingHealthResponseReader struct {
	io.Reader
	read int
}

func (r *countingHealthResponseReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func TestCompanionHealthResponseRequiresWholeBoundedSuccessfulDocument(t *testing.T) {
	const valid = `{"ok":true,"status":"live"}`
	for _, test := range []struct {
		name   string
		body   string
		status int
		want   bool
	}{
		{"live", valid, http.StatusOK, true},
		{"additional metadata", `{"ok":true,"status":"live","details":{"up":true}}`, http.StatusOK, true},
		{"trailing whitespace", valid + " \n\t", http.StatusOK, true},
		{"exact size", valid + strings.Repeat(" ", companionHealthResponseLimit-len(valid)), http.StatusOK, true},
		{"trailing junk", valid + "invalid", http.StatusOK, false},
		{"second document", valid + `{"ok":false,"status":"down"}`, http.StatusOK, false},
		{"oversized whitespace", valid + strings.Repeat(" ", companionHealthResponseLimit), http.StatusOK, false},
		{"oversized suffix", valid + strings.Repeat("x", companionHealthResponseLimit*2), http.StatusOK, false},
		{"not healthy", `{"ok":false,"status":"live"}`, http.StatusOK, false},
		{"not live", `{"ok":true,"status":"down"}`, http.StatusOK, false},
		{"bad HTTP status", valid, http.StatusServiceUnavailable, false},
		{"null", `null`, http.StatusOK, false},
		{"truncated", `{"ok":true`, http.StatusOK, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &countingHealthResponseReader{Reader: strings.NewReader(test.body)}
			response := &http.Response{StatusCode: test.status, Body: io.NopCloser(reader)}
			if got := companionHealthResponseOK(response); got != test.want {
				t.Fatalf("health accepted=%v, want %v", got, test.want)
			}
			if reader.read > companionHealthResponseLimit+1 {
				t.Fatalf("unbounded health response read: %d", reader.read)
			}
		})
	}
}

func TestCompanionHealthResponseRejectsReadFailureAfterValidPrefix(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(io.MultiReader(strings.NewReader(`{"ok":true,"status":"live"}`), healthResponseReadFailure{})),
	}
	if companionHealthResponseOK(response) {
		t.Fatal("health response accepted unreadable trailing data")
	}
}

func TestCompanionHealthResponseRejectsMissingResponseOrBody(t *testing.T) {
	if companionHealthResponseOK(nil) || companionHealthResponseOK(&http.Response{StatusCode: http.StatusOK}) {
		t.Fatal("missing health response accepted")
	}
}

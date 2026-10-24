package openclawmaintenance

import (
	"encoding/json"
	"io"
	"net/http"
)

const companionHealthResponseLimit = 4096

func companionHealthResponseOK(response *http.Response) bool {
	if response == nil || response.Body == nil || response.StatusCode != http.StatusOK {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, companionHealthResponseLimit+1))
	if err != nil || len(data) > companionHealthResponseLimit {
		return false
	}
	var health struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	return json.Unmarshal(data, &health) == nil && health.OK && health.Status == "live"
}

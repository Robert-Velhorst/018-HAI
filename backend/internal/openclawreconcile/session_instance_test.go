package openclawreconcile

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
)

func TestReceiptSessionInstanceRoundTripStaysPrivate(t *testing.T) {
	entry := models.OpenClawGatewaySessionReceipt{}
	value := reflect.ValueOf(&entry).Elem()
	for name, text := range map[string]string{"SessionID": "private-instance-123", "RequestedModel": "ollama/qwen3:14b"} {
		field := value.FieldByName(name)
		if !field.IsValid() || field.Kind() != reflect.String {
			t.Fatalf("durable receipt has no %s", name)
		}
		field.SetString(text)
	}
	encoded, _ := json.Marshal(receiptFromModel(entry))
	if !strings.Contains(string(encoded), `"SessionID":"private-instance-123"`) || !strings.Contains(string(encoded), `"RequestedModel":"ollama/qwen3:14b"`) {
		t.Fatalf("private receipt mapping lost instance/model: %s", encoded)
	}
	public, _ := json.Marshal(entry)
	if strings.Contains(string(public), "private-instance") || strings.Contains(string(public), "ollama/") {
		t.Fatalf("database model exposes private execution identity: %s", public)
	}
}

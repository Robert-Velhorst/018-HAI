package a2abridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxJSONRPCBody int64 = 16 << 10

const a2aVersion = "1.0"

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Status(c *gin.Context) { c.JSON(http.StatusOK, h.service.Status()) }

// AgentCard is intentionally discoverable only when the local bridge is fully
// configured. It advertises planning semantics and authentication, not HAI's
// internal tools, users, sources, or configuration.
func (h *Handler) AgentCard(c *gin.Context) {
	card, err := h.service.AgentCard()
	if errors.Is(err, ErrUnavailable) {
		c.Status(http.StatusNotFound)
		return
	}
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("ETag", `"hai-a2a-controlled-planning-1.0.2"`)
	c.JSON(http.StatusOK, card)
}

func (h *Handler) Send(c *gin.Context) {
	if !h.service.Authorize(bearerToken(c.GetHeader("Authorization"))) {
		// A disabled bridge and an invalid token intentionally have the same
		// response, so the endpoint cannot become an environment oracle.
		c.Status(http.StatusNotFound)
		return
	}
	if !acceptsJSON(c.GetHeader("Content-Type")) {
		writeRPCError(c, nil, -32600, "JSON-RPC requests must use application/json")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONRPCBody)
	var request jsonRPCRequest
	if err := decodeStrictJSON(c.Request.Body, &request); err != nil {
		writeRPCError(c, nil, -32600, "invalid JSON-RPC request")
		return
	}
	if request.JSONRPC != "2.0" || !validJSONRPCID(request.ID) {
		writeRPCError(c, request.ID, -32600, "JSON-RPC 2.0 request id is required")
		return
	}
	if strings.TrimSpace(c.GetHeader("A2A-Version")) != a2aVersion {
		writeRPCError(c, request.ID, -32009, "A2A-Version 1.0 is required by this bridge")
		return
	}
	if request.Method != "SendMessage" {
		writeRPCError(c, request.ID, -32601, "only SendMessage is supported by this planning bridge")
		return
	}
	text, err := taskText(request.Params)
	if err != nil {
		writeRPCError(c, request.ID, -32602, "SendMessage requires one bounded ROLE_USER text message with a messageId")
		return
	}
	proposal, err := h.service.Draft(text)
	if errors.Is(err, ErrInvalidInput) {
		writeRPCError(c, request.ID, -32602, "task text must be non-empty and at most 4096 characters")
		return
	}
	if err != nil {
		writeRPCError(c, request.ID, -32603, "HAI could not create a controlled planning draft")
		return
	}
	c.JSON(http.StatusOK, jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: sendMessageResponse{Task: a2aTask{
		ID: uuid.NewString(), ContextID: uuid.NewString(),
		Status: taskStatus{State: "TASK_STATE_COMPLETED", Timestamp: time.Now().UTC().Format(time.RFC3339Nano)},
		Artifacts: []a2aArtifact{{
			ArtifactID: uuid.NewString(), Name: "hai-controlled-planning-proposal",
			Description: "Non-executable planning draft. Review it in HAI before creating, approving, or running any work.",
			Parts:       []a2aOutputPart{{Data: proposal, MediaType: "application/json"}},
		}},
	}}})
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type sendParams struct {
	Message a2aMessage `json:"message"`
}

type a2aMessage struct {
	MessageID  string    `json:"messageId"`
	ContextID  string    `json:"contextId,omitempty"`
	TaskID     string    `json:"taskId,omitempty"`
	Role       string    `json:"role"`
	Parts      []a2aPart `json:"parts"`
	Metadata   any       `json:"metadata,omitempty"`
	Extensions []string  `json:"extensions,omitempty"`
}

type a2aPart struct {
	Text      *string         `json:"text,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
	URL       *string         `json:"url,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Filename  string          `json:"filename,omitempty"`
	MediaType string          `json:"mediaType,omitempty"`
	Metadata  any             `json:"metadata,omitempty"`
}

type a2aOutputPart struct {
	Data      any    `json:"data"`
	MediaType string `json:"mediaType,omitempty"`
}

type a2aTask struct {
	ID        string        `json:"id"`
	ContextID string        `json:"contextId"`
	Status    taskStatus    `json:"status"`
	Artifacts []a2aArtifact `json:"artifacts"`
}

type taskStatus struct {
	State     string `json:"state"`
	Timestamp string `json:"timestamp"`
}

type a2aArtifact struct {
	ArtifactID  string          `json:"artifactId"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parts       []a2aOutputPart `json:"parts"`
}

type sendMessageResponse struct {
	Task a2aTask `json:"task"`
}

func taskText(raw json.RawMessage) (string, error) {
	var params sendParams
	if len(raw) == 0 || decodeStrictJSON(bytes.NewReader(raw), &params) != nil || strings.TrimSpace(params.Message.Role) != "ROLE_USER" || !validMessageID(params.Message.MessageID) || params.Message.ContextID != "" || params.Message.TaskID != "" || params.Message.Metadata != nil || len(params.Message.Extensions) != 0 || len(params.Message.Parts) == 0 || len(params.Message.Parts) > 4 {
		return "", ErrInvalidInput
	}
	parts := make([]string, 0, len(params.Message.Parts))
	for _, part := range params.Message.Parts {
		if part.Text == nil || strings.TrimSpace(*part.Text) == "" || len(part.Raw) != 0 || part.URL != nil || len(part.Data) != 0 || part.Filename != "" || (part.MediaType != "" && !strings.EqualFold(part.MediaType, "text/plain")) || part.Metadata != nil {
			return "", ErrInvalidInput
		}
		parts = append(parts, *part.Text)
	}
	text := normalize(strings.Join(parts, " "))
	if text == "" || utf8.RuneCountInString(text) > 4096 {
		return "", ErrInvalidInput
	}
	return text, nil
}

func decodeStrictJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrInvalidInput
		}
		return err
	}
	return nil
}

func validJSONRPCID(raw json.RawMessage) bool {
	if len(raw) == 0 || len(raw) > 256 {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return utf8.ValidString(typed) && utf8.RuneCountInString(typed) <= 255
	case json.Number:
		return len(typed.String()) <= 64
	default:
		return false
	}
}

func acceptsJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func validMessageID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && utf8.RuneCountInString(value) <= 255 && !strings.ContainsAny(value, "\r\n")
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func writeRPCError(c *gin.Context, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	c.JSON(http.StatusBadRequest, jsonRPCResponse{JSONRPC: "2.0", ID: id, Error: &jsonRPCError{Code: code, Message: message}})
}

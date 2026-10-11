package a2abridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	maxJSONRPCBody int64 = 16 << 10
	replayCacheMax       = 128
	replayCacheTTL       = 10 * time.Minute
)

const a2aVersion = "1.0"

type Handler struct {
	service *Service

	replayMu    sync.Mutex
	replayCache map[string]*replayEntry
}

type replayEntry struct {
	fingerprint [sha256.Size]byte
	response    *sendMessageResponse
	done        chan struct{}
	createdAt   time.Time
	expiresAt   time.Time
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service, replayCache: make(map[string]*replayEntry)}
}

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
	// Invalid request IDs are not safe to reflect into an error response.
	if request.JSONRPC != "2.0" || !validRequestID(request.ID) || !validJSONRPCID(request.ID) {
		// An invalid JSON-RPC id cannot safely be echoed in an error response.
		// Use null as required for an invalid request rather than reflecting an
		// object or array supplied by an untrusted peer.
		writeRPCError(c, nil, -32600, "JSON-RPC 2.0 request id is required")
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
	message, err := taskInput(request.Params)
	if err != nil {
		writeRPCError(c, request.ID, -32602, "SendMessage requires one bounded ROLE_USER text message with a messageId")
		return
	}
	for {
		cached, plan, conflict, overloaded, pending := h.beginReplay(message.MessageID, message.Text)
		if conflict {
			writeRPCError(c, request.ID, -32602, "messageId was already used for a different request")
			return
		}
		if overloaded {
			writeRPCError(c, request.ID, -32001, "HAI local planning bridge is busy; retry with the same messageId")
			return
		}
		if cached != nil {
			c.JSON(http.StatusOK, jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: *cached})
			return
		}
		if !plan {
			select {
			case <-pending:
				continue
			case <-c.Request.Context().Done():
				return
			}
		}
		break
	}

	proposal, err := h.service.Draft(message.Text)
	if errors.Is(err, ErrInvalidInput) {
		h.failReplay(message.MessageID)
		writeRPCError(c, request.ID, -32602, "task text must be non-empty and at most 4096 characters")
		return
	}
	if err != nil {
		h.failReplay(message.MessageID)
		writeRPCError(c, request.ID, -32603, "HAI could not create a controlled planning draft")
		return
	}
	result := sendMessageResponse{Task: a2aTask{
		ID: uuid.NewString(), ContextID: uuid.NewString(),
		Status: taskStatus{State: "TASK_STATE_COMPLETED", Timestamp: time.Now().UTC().Format(time.RFC3339Nano)},
		Artifacts: []a2aArtifact{{
			ArtifactID: uuid.NewString(), Name: "hai-controlled-planning-proposal",
			Description: "Non-executable planning draft. Review it in HAI before creating, approving, or running any work.",
			Parts:       []a2aOutputPart{{Data: proposal, MediaType: "application/json"}},
		}},
	}}
	h.completeReplay(message.MessageID, result)
	c.JSON(http.StatusOK, jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Result: result})
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

type incomingTask struct {
	MessageID string
	Text      string
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

func taskInput(raw json.RawMessage) (incomingTask, error) {
	var params sendParams
	if len(raw) == 0 || decodeStrictJSON(bytes.NewReader(raw), &params) != nil || strings.TrimSpace(params.Message.Role) != "ROLE_USER" || !validMessageID(params.Message.MessageID) || params.Message.ContextID != "" || params.Message.TaskID != "" || params.Message.Metadata != nil || len(params.Message.Extensions) != 0 || len(params.Message.Parts) == 0 || len(params.Message.Parts) > 4 {
		return incomingTask{}, ErrInvalidInput
	}
	parts := make([]string, 0, len(params.Message.Parts))
	for _, part := range params.Message.Parts {
		if part.Text == nil || strings.TrimSpace(*part.Text) == "" || len(part.Raw) != 0 || part.URL != nil || len(part.Data) != 0 || part.Filename != "" || (part.MediaType != "" && !strings.EqualFold(part.MediaType, "text/plain")) || part.Metadata != nil {
			return incomingTask{}, ErrInvalidInput
		}
		parts = append(parts, *part.Text)
	}
	text := normalize(strings.Join(parts, " "))
	if text == "" || utf8.RuneCountInString(text) > 4096 {
		return incomingTask{}, ErrInvalidInput
	}
	return incomingTask{MessageID: strings.TrimSpace(params.Message.MessageID), Text: text}, nil
}

// beginReplay provides at-least-once message delivery without duplicated local
// planning work. The cache is process-local by design: the A2A bridge is a
// local advisory boundary, not HAI's durable workflow authority.
func (h *Handler) beginReplay(messageID, text string) (cached *sendMessageResponse, plan, conflict, overloaded bool, pending <-chan struct{}) {
	fingerprint := sha256.Sum256([]byte(text))
	now := time.Now().UTC()

	h.replayMu.Lock()
	defer h.replayMu.Unlock()
	h.pruneReplayLocked(now)
	if entry := h.replayCache[messageID]; entry != nil {
		if entry.fingerprint != fingerprint {
			return nil, false, true, false, nil
		}
		if entry.response != nil {
			return entry.response, false, false, false, nil
		}
		return nil, false, false, false, entry.done
	}
	if len(h.replayCache) >= replayCacheMax {
		return nil, false, false, true, nil
	}
	h.replayCache[messageID] = &replayEntry{
		fingerprint: fingerprint, done: make(chan struct{}), createdAt: now,
	}
	return nil, true, false, false, nil
}

func (h *Handler) completeReplay(messageID string, result sendMessageResponse) {
	h.replayMu.Lock()
	defer h.replayMu.Unlock()
	entry := h.replayCache[messageID]
	if entry == nil || entry.response != nil {
		return
	}
	entry.response = &result
	entry.expiresAt = time.Now().UTC().Add(replayCacheTTL)
	close(entry.done)
}

func (h *Handler) failReplay(messageID string) {
	h.replayMu.Lock()
	defer h.replayMu.Unlock()
	entry := h.replayCache[messageID]
	if entry == nil || entry.response != nil {
		return
	}
	delete(h.replayCache, messageID)
	close(entry.done)
}

func (h *Handler) pruneReplayLocked(now time.Time) {
	for messageID, entry := range h.replayCache {
		if entry.response != nil && !entry.expiresAt.After(now) {
			delete(h.replayCache, messageID)
		}
	}
	if len(h.replayCache) < replayCacheMax {
		return
	}
	var oldestID string
	var oldest time.Time
	for messageID, entry := range h.replayCache {
		if entry.response == nil || (!oldest.IsZero() && !entry.createdAt.Before(oldest)) {
			continue
		}
		oldestID, oldest = messageID, entry.createdAt
	}
	if oldestID != "" {
		delete(h.replayCache, oldestID)
	}
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

func validRequestID(raw json.RawMessage) bool {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" || len(raw) > 255 {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return false
	}
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= 128 && !strings.ContainsAny(value, "\r\n")
	case json.Number:
		return strings.TrimSpace(value.String()) != ""
	default:
		return false
	}
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

// Package semantic provides HAI's optional local pgvector retrieval path.
// Source remains the provenance authority; semantic retrieval only enriches
// eligible cached extractions after a local embedding server is configured.
package semantic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/identity"
	"automation-hub-backend/internal/infra"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	defaultInputLimit = 12000
	defaultTimeout    = 10 * time.Second
)

type SearchRequest struct {
	OwnerIdentity    string
	Query            string
	ProjectKey       string
	Limit            int
	IncludeSensitive bool
}

type Match struct {
	Extraction models.SourceExtraction
	Similarity float64
}

// MemorySearchRequest keeps HAI's editable context-memory retrieval inside the
// same local embedding boundary as source extraction retrieval. Owner and
// project filtering are enforced in the database query before a match returns.
type MemorySearchRequest struct {
	OwnerIdentity string
	Query         string
	ProjectKey    string
	Limit         int
}

type MemoryMatch struct {
	Memory     models.ContextMemory
	Similarity float64
}

// Service can be disabled. Callers retain keyword retrieval when a local
// embedding service has not been configured or is not reachable.
type Service interface {
	Enabled() bool
	Reason() string
	Index(context.Context, *models.SourceExtraction) error
	Search(context.Context, SearchRequest) ([]Match, error)
	IndexMemory(context.Context, *models.ContextMemory) error
	DeleteMemory(context.Context, uuid.UUID) error
	SearchMemory(context.Context, MemorySearchRequest) ([]MemoryMatch, error)
}

type Config struct {
	Enabled        bool
	BaseURL        string
	Model          string
	APIKey         string
	InputLimit     int
	RequestTimeout time.Duration
}

type disabledService struct{ reason string }

func (s disabledService) Enabled() bool  { return false }
func (s disabledService) Reason() string { return s.reason }
func (s disabledService) Index(context.Context, *models.SourceExtraction) error {
	return fmt.Errorf("semantic retrieval is disabled: %s", s.reason)
}
func (s disabledService) Search(context.Context, SearchRequest) ([]Match, error) {
	return nil, fmt.Errorf("semantic retrieval is disabled: %s", s.reason)
}
func (s disabledService) IndexMemory(context.Context, *models.ContextMemory) error {
	return fmt.Errorf("semantic retrieval is disabled: %s", s.reason)
}
func (s disabledService) DeleteMemory(context.Context, uuid.UUID) error {
	return fmt.Errorf("semantic retrieval is disabled: %s", s.reason)
}
func (s disabledService) SearchMemory(context.Context, MemorySearchRequest) ([]MemoryMatch, error) {
	return nil, fmt.Errorf("semantic retrieval is disabled: %s", s.reason)
}

type service struct {
	db     *gorm.DB
	config Config
	client *http.Client
}

// NewServiceFromEnv is fail-closed. Bad configuration disables semantic
// retrieval instead of preventing the local control plane from serving the
// existing provenance-preserving keyword search path.
func NewServiceFromEnv() Service {
	config := ConfigFromEnv()
	if !config.Enabled {
		return disabledService{reason: "HAI_SEMANTIC_RETRIEVAL_ENABLED is false or missing"}
	}
	db, err := infra.GetDefaultDB()
	if err != nil {
		return disabledService{reason: "database unavailable: " + compactError(err)}
	}
	service, err := NewService(db, config)
	if err != nil {
		return disabledService{reason: compactError(err)}
	}
	return service
}

func ConfigFromEnv() Config {
	inputLimit := defaultInputLimit
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HAI_EMBEDDING_INPUT_LIMIT"))); err == nil && value >= 256 && value <= 100000 {
		inputLimit = value
	}
	timeout := defaultTimeout
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HAI_EMBEDDING_TIMEOUT_SECONDS"))); err == nil && value >= 1 && value <= 60 {
		timeout = time.Duration(value) * time.Second
	}
	return Config{
		Enabled:        strings.EqualFold(strings.TrimSpace(os.Getenv("HAI_SEMANTIC_RETRIEVAL_ENABLED")), "true"),
		BaseURL:        strings.TrimSpace(os.Getenv("HAI_EMBEDDING_BASE_URL")),
		Model:          strings.TrimSpace(os.Getenv("HAI_EMBEDDING_MODEL")),
		APIKey:         strings.TrimSpace(os.Getenv("HAI_EMBEDDING_API_KEY")),
		InputLimit:     inputLimit,
		RequestTimeout: timeout,
	}
}

func NewService(db *gorm.DB, config Config) (Service, error) {
	if db == nil {
		return nil, fmt.Errorf("semantic: database is required")
	}
	if !config.Enabled {
		return disabledService{reason: "disabled by configuration"}, nil
	}
	if config.BaseURL == "" || config.Model == "" {
		return nil, fmt.Errorf("semantic: HAI_EMBEDDING_BASE_URL and HAI_EMBEDDING_MODEL are required")
	}
	if err := validateLocalURL(config.BaseURL); err != nil {
		return nil, fmt.Errorf("semantic: %w", err)
	}
	if config.InputLimit <= 0 {
		config.InputLimit = defaultInputLimit
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultTimeout
	}
	if err := ensureSchema(db); err != nil {
		return nil, err
	}
	return &service{db: db, config: config, client: &http.Client{Timeout: config.RequestTimeout}}, nil
}

func (s *service) Enabled() bool  { return true }
func (s *service) Reason() string { return "local pgvector retrieval enabled" }

func ensureSchema(db *gorm.DB) error {
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
		return fmt.Errorf("semantic: enable pgvector extension: %w", err)
	}
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS semantic_embeddings (
			extraction_id UUID PRIMARY KEY REFERENCES source_extractions(id) ON DELETE CASCADE,
			source_id UUID NOT NULL REFERENCES connected_sources(id) ON DELETE CASCADE,
			model_id VARCHAR(255) NOT NULL,
			content_hash VARCHAR(64) NOT NULL,
			embedding vector NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`).Error; err != nil {
		return fmt.Errorf("semantic: create embedding table: %w", err)
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_semantic_embeddings_source_model ON semantic_embeddings (source_id, model_id)`).Error; err != nil {
		return fmt.Errorf("semantic: create embedding lookup index: %w", err)
	}
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS semantic_memory_embeddings (
			memory_id UUID PRIMARY KEY REFERENCES context_memories(id) ON DELETE CASCADE,
			owner_identity VARCHAR(255) NOT NULL DEFAULT '',
			project_key VARCHAR(255) NOT NULL DEFAULT '',
			model_id VARCHAR(255) NOT NULL,
			content_hash VARCHAR(64) NOT NULL,
			embedding vector NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`).Error; err != nil {
		return fmt.Errorf("semantic: create memory embedding table: %w", err)
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_semantic_memory_embeddings_scope ON semantic_memory_embeddings (owner_identity, project_key, model_id)`).Error; err != nil {
		return fmt.Errorf("semantic: create memory embedding lookup index: %w", err)
	}
	return nil
}

func (s *service) Index(ctx context.Context, extraction *models.SourceExtraction) error {
	if extraction == nil || extraction.ID == uuid.Nil || extraction.SourceID == uuid.Nil {
		return fmt.Errorf("semantic: extraction and source IDs are required")
	}
	text := embeddingText(extraction)
	if text == "" {
		return fmt.Errorf("semantic: extraction has no indexable text")
	}
	vector, err := s.embed(ctx, text)
	if err != nil {
		return err
	}
	contentHash := strings.TrimSpace(extraction.ContentHash)
	if contentHash == "" {
		sum := sha256.Sum256([]byte(text))
		contentHash = hex.EncodeToString(sum[:])
	}
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO semantic_embeddings (extraction_id, source_id, model_id, content_hash, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?::vector, NOW(), NOW())
		ON CONFLICT (extraction_id) DO UPDATE SET
			source_id = EXCLUDED.source_id, model_id = EXCLUDED.model_id,
			content_hash = EXCLUDED.content_hash, embedding = EXCLUDED.embedding, updated_at = NOW()`,
		extraction.ID, extraction.SourceID, s.config.Model, contentHash, vectorLiteral(vector)).Error
}

func (s *service) Search(ctx context.Context, request SearchRequest) ([]Match, error) {
	if strings.TrimSpace(request.Query) == "" {
		return []Match{}, nil
	}
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	vector, err := s.embed(ctx, request.Query)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT se.id AS extraction_id, 1 - (emb.embedding <=> ?::vector) AS similarity
		FROM semantic_embeddings emb
		JOIN source_extractions se ON se.id = emb.extraction_id
		JOIN connected_sources cs ON cs.id = se.source_id
		WHERE emb.model_id = ?
		  AND se.archived = FALSE
		  AND cs.revoked_at IS NULL
		  AND LOWER(COALESCE(cs.status, '')) <> 'revoked'`
	args := []any{vectorLiteral(vector), s.config.Model}
	if !request.IncludeSensitive {
		query += ` AND se.sensitive = FALSE`
	}
	if projectKey := strings.TrimSpace(request.ProjectKey); projectKey != "" {
		query += ` AND se.project_key = ?`
		args = append(args, projectKey)
	}
	if owner := strings.TrimSpace(request.OwnerIdentity); owner != "" {
		if identity.CanReadLegacyOwnerlessData(owner) {
			query += ` AND (cs.owner_identity = ? OR cs.owner_identity = '' OR cs.owner_identity IS NULL)`
		} else {
			query += ` AND cs.owner_identity = ?`
		}
		args = append(args, owner)
	}
	query += ` ORDER BY emb.embedding <=> ?::vector ASC LIMIT ?`
	args = append(args, vectorLiteral(vector), limit)

	type row struct {
		ExtractionID uuid.UUID `gorm:"column:extraction_id"`
		Similarity   float64   `gorm:"column:similarity"`
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("semantic: query embeddings: %w", err)
	}
	if len(rows) == 0 {
		return []Match{}, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ExtractionID)
	}
	var extractions []models.SourceExtraction
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&extractions).Error; err != nil {
		return nil, fmt.Errorf("semantic: load extraction results: %w", err)
	}
	byID := make(map[uuid.UUID]models.SourceExtraction, len(extractions))
	for _, extraction := range extractions {
		byID[extraction.ID] = extraction
	}
	matches := make([]Match, 0, len(rows))
	for _, row := range rows {
		if extraction, ok := byID[row.ExtractionID]; ok {
			matches = append(matches, Match{Extraction: extraction, Similarity: round(row.Similarity, 3)})
		}
	}
	return matches, nil
}

func (s *service) IndexMemory(ctx context.Context, memory *models.ContextMemory) error {
	if memory == nil || memory.ID == uuid.Nil {
		return fmt.Errorf("semantic: memory ID is required")
	}
	if memory.Archived {
		return s.DeleteMemory(ctx, memory.ID)
	}
	text := memoryEmbeddingText(memory)
	if text == "" {
		return fmt.Errorf("semantic: memory has no indexable text")
	}
	vector, err := s.embed(ctx, text)
	if err != nil {
		return err
	}
	contentHash := strings.TrimSpace(memory.ContentHash)
	if contentHash == "" {
		sum := sha256.Sum256([]byte(text))
		contentHash = hex.EncodeToString(sum[:])
	}
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO semantic_memory_embeddings (memory_id, owner_identity, project_key, model_id, content_hash, embedding, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?::vector, NOW(), NOW())
		ON CONFLICT (memory_id) DO UPDATE SET
			owner_identity = EXCLUDED.owner_identity, project_key = EXCLUDED.project_key,
			model_id = EXCLUDED.model_id, content_hash = EXCLUDED.content_hash,
			embedding = EXCLUDED.embedding, updated_at = NOW()`,
		memory.ID, strings.TrimSpace(memory.OwnerIdentity), strings.TrimSpace(memory.ProjectKey), s.config.Model, contentHash, vectorLiteral(vector)).Error
}

func (s *service) DeleteMemory(ctx context.Context, memoryID uuid.UUID) error {
	if memoryID == uuid.Nil {
		return fmt.Errorf("semantic: memory ID is required")
	}
	return s.db.WithContext(ctx).Exec(`DELETE FROM semantic_memory_embeddings WHERE memory_id = ?`, memoryID).Error
}

func (s *service) SearchMemory(ctx context.Context, request MemorySearchRequest) ([]MemoryMatch, error) {
	if strings.TrimSpace(request.Query) == "" {
		return []MemoryMatch{}, nil
	}
	limit := request.Limit
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	vector, err := s.embed(ctx, request.Query)
	if err != nil {
		return nil, err
	}
	query := `
		SELECT cm.id AS memory_id, 1 - (emb.embedding <=> ?::vector) AS similarity
		FROM semantic_memory_embeddings emb
		JOIN context_memories cm ON cm.id = emb.memory_id
		WHERE emb.model_id = ? AND cm.archived = FALSE`
	args := []any{vectorLiteral(vector), s.config.Model}
	if projectKey := strings.TrimSpace(request.ProjectKey); projectKey != "" {
		query += ` AND (cm.project_key = ? OR cm.project_key = '' OR cm.project_key IS NULL)`
		args = append(args, projectKey)
	}
	if owner := strings.TrimSpace(request.OwnerIdentity); owner != "" {
		if identity.CanReadLegacyOwnerlessData(owner) {
			query += ` AND (cm.owner_identity = ? OR cm.owner_identity = '' OR cm.owner_identity IS NULL)`
		} else {
			query += ` AND cm.owner_identity = ?`
		}
		args = append(args, owner)
	}
	query += ` ORDER BY emb.embedding <=> ?::vector ASC LIMIT ?`
	args = append(args, vectorLiteral(vector), limit)

	type row struct {
		MemoryID   uuid.UUID `gorm:"column:memory_id"`
		Similarity float64   `gorm:"column:similarity"`
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("semantic: query memory embeddings: %w", err)
	}
	if len(rows) == 0 {
		return []MemoryMatch{}, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.MemoryID)
	}
	var memories []models.ContextMemory
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&memories).Error; err != nil {
		return nil, fmt.Errorf("semantic: load memory results: %w", err)
	}
	byID := make(map[uuid.UUID]models.ContextMemory, len(memories))
	for _, memory := range memories {
		byID[memory.ID] = memory
	}
	matches := make([]MemoryMatch, 0, len(rows))
	for _, row := range rows {
		if memory, ok := byID[row.MemoryID]; ok {
			matches = append(matches, MemoryMatch{Memory: memory, Similarity: round(row.Similarity, 3)})
		}
	}
	return matches, nil
}

func (s *service) embed(ctx context.Context, input string) ([]float64, error) {
	payload, err := json.Marshal(map[string]any{"model": s.config.Model, "input": trimRunes(strings.TrimSpace(input), s.config.InputLimit)})
	if err != nil {
		return nil, fmt.Errorf("semantic: marshal embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.config.BaseURL, "/")+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("semantic: create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("semantic: local embedding request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("semantic: local embedding endpoint returned HTTP %d", response.StatusCode)
	}
	var decoded struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("semantic: decode embedding response: %w", err)
	}
	if len(decoded.Data) != 1 || len(decoded.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("semantic: embedding response contained no vector")
	}
	for _, value := range decoded.Data[0].Embedding {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("semantic: embedding response contained a non-finite value")
		}
	}
	return decoded.Data[0].Embedding, nil
}

func embeddingText(extraction *models.SourceExtraction) string {
	return strings.TrimSpace(strings.Join([]string{extraction.Text, extraction.Summary, extraction.Entities, extraction.Tasks, extraction.Decisions}, "\n"))
}

func memoryEmbeddingText(memory *models.ContextMemory) string {
	if memory == nil {
		return ""
	}
	return strings.TrimSpace(strings.Join([]string{memory.Content, memory.Summary, memory.Tags}, "\n"))
}

func vectorLiteral(values []float64) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.FormatFloat(value, 'g', -1, 64)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func validateLocalURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("embedding endpoint must be an absolute HTTP(S) URL without credentials")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || host == "host.docker.internal" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("embedding endpoint must use localhost, loopback, or host.docker.internal")
}

func trimRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func compactError(err error) string {
	value := strings.TrimSpace(err.Error())
	if len(value) <= 180 {
		return value
	}
	return value[:177] + "..."
}

func round(value float64, places int) float64 {
	multiplier := math.Pow10(places)
	return math.Round(value*multiplier) / multiplier
}

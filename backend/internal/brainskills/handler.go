package brainskills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"automation-hub-backend/internal/identity"
	"github.com/gin-gonic/gin"
)

const (
	maxSelectionBodyBytes   = 1024
	maxGuidancePreviewBytes = MaxGuidanceSummaryBytes
)

var ErrCatalogReviewStale = errors.New("brain skill catalog changed since review")

// SelectionState is the owner-specific view of a skill selection. A stale
// selection must set NeedsReapproval; the handler will never report it as
// enabled even if a faulty store returns both flags as true.
type SelectionState struct {
	Enabled                        bool
	NeedsReapproval                bool
	Decision                       *SelectionDecisionMetadata
	SupersededByConcurrentDecision bool
}

// SelectionDecisionMetadata contains only the public audit details needed to
// inspect the latest authenticated selection decision. It deliberately omits
// owner scope and guidance content. The prior guidance hash is internal-only
// and is returned solely by the approval-authorized preview endpoint.
type SelectionDecisionMetadata struct {
	ID             int64     `json:"id"`
	ActorIdentity  string    `json:"actorIdentity"`
	DecidedAt      time.Time `json:"decidedAt"`
	GuidanceSHA256 string    `json:"-"`
}

// OwnerSelectionStore is the only persistence capability needed by this HTTP
// handler. The server resolves and supplies the full current catalog pin so a
// client cannot choose or forge a source or guidance hash. Implementations
// must support concurrent requests.
type OwnerSelectionStore interface {
	State(ctx context.Context, ownerIdentity string, skill Skill) (SelectionState, error)
	Set(ctx context.Context, ownerIdentity string, skill Skill, enabled bool, reviewedCatalogFingerprint string) (SelectionState, error)
}

// OwnerSelectionBatchStore is an optional inventory capability. It must
// return a state for every requested skill, including unselected skills.
// Stores that do not implement it retain the per-skill fallback above.
type OwnerSelectionBatchStore interface {
	StatesForOwner(ctx context.Context, ownerIdentity string, skills []Skill) (map[string]SelectionState, error)
}

// Handler exposes a fixed, metadata-only catalog and owner-scoped selection
// operations. It never returns guidance bodies or executes upstream assets.
type Handler struct {
	store   OwnerSelectionStore
	catalog Catalog
}

func NewHandler(store OwnerSelectionStore) *Handler {
	return &Handler{store: store, catalog: DefaultCatalog()}
}

type inventoryResponse struct {
	SourceRepository   string                `json:"sourceRepository"`
	SourceCommit       string                `json:"sourceCommit"`
	SourceCommitDate   string                `json:"sourceCommitDate"`
	CatalogFingerprint string                `json:"catalogFingerprint"`
	Skills             []inventorySkillEntry `json:"skills"`
}

type inventorySkillEntry struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	Description     string                     `json:"description"`
	License         string                     `json:"license"`
	LicenseURL      string                     `json:"licenseURL"`
	LicensePath     string                     `json:"licensePath"`
	LicenseSHA256   string                     `json:"licenseSHA256"`
	SourceURL       string                     `json:"sourceURL"`
	SourcePath      string                     `json:"sourcePath"`
	SourceSHA256    string                     `json:"sourceSHA256"`
	GuidanceSHA256  string                     `json:"guidanceSHA256"`
	Scope           string                     `json:"scope"`
	Status          string                     `json:"status"`
	Enabled         bool                       `json:"enabled"`
	NeedsReapproval bool                       `json:"needsReapproval"`
	Decision        *SelectionDecisionMetadata `json:"selectionDecision,omitempty"`
}

type selectionResponse struct {
	Enabled                        bool                       `json:"enabled"`
	NeedsReapproval                bool                       `json:"needsReapproval"`
	Decision                       *SelectionDecisionMetadata `json:"selectionDecision,omitempty"`
	SupersededByConcurrentDecision bool                       `json:"supersededByConcurrentDecision,omitempty"`
}

type selectionRequest struct {
	enabled                    bool
	reviewedGuidanceSHA256     string
	reviewedCatalogFingerprint string
}

type GuidancePreviewResponse struct {
	SkillID                        string `json:"skillId"`
	CurrentGuidance                string `json:"currentGuidance"`
	CurrentGuidanceSHA256          string `json:"currentGuidanceSHA256"`
	CatalogFingerprint             string `json:"catalogFingerprint"`
	AuthorityBoundary              string `json:"authorityBoundary"`
	PreviousApprovedGuidanceSHA256 string `json:"previousApprovedGuidanceSHA256,omitempty"`
	PreviousGuidanceTextStored     bool   `json:"previousGuidanceTextStored"`
	PreviousGuidanceTextLimitation string `json:"previousGuidanceTextLimitation,omitempty"`
}

// Inventory returns the fixed, reviewed skill metadata and the caller's
// selection state for each skill. A partial inventory is never returned when
// the owner store fails.
func (h *Handler) Inventory(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	owner, ok := verifiedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required"})
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "skill selection is unavailable"})
		return
	}

	skills := h.catalog.List()
	response := inventoryResponse{
		SourceRepository:   RepositoryURL,
		SourceCommit:       SourceCommit,
		SourceCommitDate:   SourceCommitDate,
		CatalogFingerprint: h.catalog.Fingerprint(),
		Skills:             make([]inventorySkillEntry, 0, len(skills)),
	}
	states := make(map[string]SelectionState, len(skills))
	if batchStore, ok := h.store.(OwnerSelectionBatchStore); ok {
		batchStates, err := batchStore.StatesForOwner(c.Request.Context(), owner, skills)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load skill selection state"})
			return
		}
		for _, skill := range skills {
			state, exists := batchStates[skill.ID]
			if !exists {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load complete skill selection state"})
				return
			}
			states[skill.ID] = state
		}
	} else {
		for _, skill := range skills {
			state, err := h.store.State(c.Request.Context(), owner, skill)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load skill selection state"})
				return
			}
			states[skill.ID] = state
		}
	}
	for _, skill := range skills {
		response.Skills = append(response.Skills, inventoryEntry(skill, states[skill.ID]))
	}
	c.JSON(http.StatusOK, response)
}

// SetSelection records the caller's owner-scoped consent for one known skill.
// Only the boolean choice and, when enabling, the reviewed guidance hash and
// catalog fingerprint are accepted from JSON; current pins come from the server.
func (h *Handler) SetSelection(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	owner, ok := verifiedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required"})
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "skill selection is unavailable"})
		return
	}

	skill, ok := h.skillByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	if !isJSONContentType(c.GetHeader("Content-Type")) {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "content type must be application/json"})
		return
	}
	if c.Request.ContentLength > maxSelectionBodyBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "selection request is too large"})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxSelectionBodyBytes))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "selection request is too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "selection request is invalid"})
		return
	}
	request, err := decodeSelectionRequest(bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "selection request must contain an enabled boolean and, when enabling, the reviewed guidance hash and catalog fingerprint"})
		return
	}
	if request.enabled && request.reviewedCatalogFingerprint != h.catalog.Fingerprint() {
		c.JSON(http.StatusConflict, gin.H{"error": "The Skills catalog or source identity changed since it was reviewed. Refresh and review the current version before enabling."})
		return
	}
	if request.enabled && request.reviewedGuidanceSHA256 != skill.GuidanceSHA256 {
		c.JSON(http.StatusConflict, gin.H{"error": "HAI-authored guidance changed since it was reviewed. Refresh and review the current guidance before enabling."})
		return
	}

	state, err := h.store.Set(c.Request.Context(), owner, skill, request.enabled, request.reviewedCatalogFingerprint)
	if err != nil {
		if errors.Is(err, ErrCatalogReviewStale) {
			c.JSON(http.StatusConflict, gin.H{"error": "The Skills catalog or source identity changed before the selection could be saved. Refresh and review the current version before enabling."})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save skill selection"})
		return
	}
	c.JSON(http.StatusOK, responseState(state))
}

// GuidancePreview returns only the bounded HAI-authored summary and its
// authority boundary to an authenticated owner. Upstream files are never loaded.
func (h *Handler) GuidancePreview(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	owner, ok := verifiedOwner(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "an authenticated owner session is required"})
		return
	}
	if h == nil || h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "skill selection is unavailable"})
		return
	}

	skill, ok := h.skillByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "skill not found"})
		return
	}
	guidance, ok := h.catalog.GuidanceFor(skill.ID)
	if !ok || guidance == "" || len(guidance) > maxGuidancePreviewBytes || !utf8.ValidString(guidance) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "current HAI-authored guidance is unavailable"})
		return
	}
	digest := sha256.Sum256([]byte(guidance))
	currentHash := hex.EncodeToString(digest[:])
	if currentHash != skill.GuidanceSHA256 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "current HAI-authored guidance failed integrity verification"})
		return
	}

	state, err := h.store.State(c.Request.Context(), owner, skill)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load skill approval state"})
		return
	}
	response := GuidancePreviewResponse{
		SkillID:                    skill.ID,
		CurrentGuidance:            guidance,
		CurrentGuidanceSHA256:      currentHash,
		CatalogFingerprint:         h.catalog.Fingerprint(),
		AuthorityBoundary:          boundary,
		PreviousGuidanceTextStored: false,
	}
	if state.NeedsReapproval {
		if state.Decision == nil || !isSHA256Hash(state.Decision.GuidanceSHA256) || state.Decision.GuidanceSHA256 == currentHash {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "previous skill approval could not be verified"})
			return
		}
		response.PreviousApprovedGuidanceSHA256 = state.Decision.GuidanceSHA256
		response.PreviousGuidanceTextLimitation = "HAI stores the hash of previously approved guidance, not its historical text. The earlier text cannot be reconstructed or shown."
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) skillByID(id string) (Skill, bool) {
	for _, skill := range h.catalog.List() {
		if skill.ID == id {
			return skill, true
		}
	}
	return Skill{}, false
}

func inventoryEntry(skill Skill, state SelectionState) inventorySkillEntry {
	selection := responseState(state)
	return inventorySkillEntry{
		ID:              skill.ID,
		Name:            skill.Name,
		Description:     skill.Purpose,
		License:         skill.License,
		LicenseURL:      skill.LicenseURL,
		LicensePath:     skill.LicensePath,
		LicenseSHA256:   skill.LicenseSHA256,
		SourceURL:       skill.SourceURL,
		SourcePath:      skill.SourcePath,
		SourceSHA256:    skill.SourceSHA256,
		GuidanceSHA256:  skill.GuidanceSHA256,
		Scope:           skill.Scope,
		Status:          skill.Status,
		Enabled:         selection.Enabled,
		NeedsReapproval: selection.NeedsReapproval,
		Decision:        selection.Decision,
	}
}

func responseState(state SelectionState) selectionResponse {
	return selectionResponse{
		Enabled:                        state.Enabled && !state.NeedsReapproval,
		NeedsReapproval:                state.NeedsReapproval,
		Decision:                       state.Decision,
		SupersededByConcurrentDecision: state.SupersededByConcurrentDecision,
	}
}

func verifiedOwner(c *gin.Context) (string, bool) {
	value, exists := c.Get(identity.ContextSubjectKey)
	if !exists {
		return "", false
	}
	owner, ok := value.(string)
	if !ok {
		return "", false
	}
	if owner == "" || owner != strings.TrimSpace(owner) || !utf8.ValidString(owner) || utf8.RuneCountInString(owner) > 255 {
		return "", false
	}
	for _, character := range owner {
		if unicode.IsControl(character) {
			return "", false
		}
	}
	return owner, true
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func decodeSelectionRequest(reader io.Reader) (selectionRequest, error) {
	decoder := json.NewDecoder(reader)

	opening, err := decoder.Token()
	if err != nil {
		return selectionRequest{}, err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return selectionRequest{}, errors.New("selection request must be a JSON object")
	}
	var request selectionRequest
	seen := make(map[string]bool, 3)
	for decoder.More() {
		field, err := decoder.Token()
		if err != nil {
			return selectionRequest{}, err
		}
		name, ok := field.(string)
		if !ok || (name != "enabled" && name != "reviewedGuidanceSHA256" && name != "reviewedCatalogFingerprint") || seen[name] {
			return selectionRequest{}, errors.New("unknown or duplicate selection field")
		}
		seen[name] = true
		value, err := decoder.Token()
		if err != nil {
			return selectionRequest{}, err
		}
		switch name {
		case "enabled":
			request.enabled, ok = value.(bool)
			if !ok {
				return selectionRequest{}, errors.New("enabled must be a boolean")
			}
		case "reviewedGuidanceSHA256":
			request.reviewedGuidanceSHA256, ok = value.(string)
			if !ok || !isSHA256Hash(request.reviewedGuidanceSHA256) {
				return selectionRequest{}, errors.New("reviewed guidance hash must be a lowercase SHA-256 digest")
			}
		case "reviewedCatalogFingerprint":
			request.reviewedCatalogFingerprint, ok = value.(string)
			if !ok || !isSHA256Hash(request.reviewedCatalogFingerprint) {
				return selectionRequest{}, errors.New("reviewed catalog fingerprint must be a lowercase SHA-256 digest")
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return selectionRequest{}, err
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return selectionRequest{}, errors.New("selection request object is incomplete")
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return selectionRequest{}, errors.New("selection request contains trailing JSON")
		}
		return selectionRequest{}, err
	}
	if !seen["enabled"] || request.enabled != (request.reviewedGuidanceSHA256 != "") ||
		request.enabled != (request.reviewedCatalogFingerprint != "") {
		return selectionRequest{}, errors.New("enabled state and reviewed guidance/catalog fingerprints do not match")
	}
	return request, nil
}

func isSHA256Hash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

package accountfeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

// AutoposterConnection is trusted server configuration, never an API request.
// Token is bound to an exact endpoint and both the HAI and Autoposter owners.
type AutoposterConnection struct {
	BaseURL     string
	Token       string `json:"-"`
	RemoteOwner string
	OwnerUserID string
	WorkspaceID string
}

var autoposterTokenPattern = regexp.MustCompile(`^hai_[A-Za-z0-9_-]+$`)

// AutoposterFromEnv opts in only when a base URL is explicitly configured.
func AutoposterFromEnv(owner, workspace string) *AutoposterConnection {
	base := strings.TrimSpace(os.Getenv("HAI_AUTOPOSTER_BASE_URL"))
	if base == "" {
		return nil
	}
	return &AutoposterConnection{BaseURL: strings.TrimRight(base, "/"),
		Token:       strings.TrimSpace(os.Getenv("HAI_AUTOPOSTER_TOKEN")),
		RemoteOwner: strings.TrimSpace(os.Getenv("HAI_AUTOPOSTER_REMOTE_OWNER")),
		OwnerUserID: owner, WorkspaceID: workspace}
}

func (a AutoposterConnection) validate() error {
	u, err := url.Parse(a.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("accountfeed: invalid Autoposter base URL")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("accountfeed: Autoposter requires HTTPS or literal loopback HTTP")
		}
	}
	if err := validateFeedURL(a.BaseURL); err != nil {
		return fmt.Errorf("accountfeed: unsafe Autoposter endpoint")
	}
	if !autoposterTokenPattern.MatchString(a.Token) || len(a.Token) > 512 {
		return fmt.Errorf("accountfeed: valid Autoposter read token required")
	}
	if a.RemoteOwner == "" || a.OwnerUserID == "" || a.WorkspaceID == "" {
		return fmt.Errorf("accountfeed: Autoposter and HAI owner/workspace binding required")
	}
	return nil
}

func (a AutoposterConnection) feedID() uuid.UUID {
	b, _ := json.Marshal([]string{"autoposter", a.BaseURL, a.RemoteOwner, a.OwnerUserID, a.WorkspaceID})
	return uuid.NewSHA1(uuid.NameSpaceURL, b)
}

func (a AutoposterConnection) matches(f Feed) bool {
	return f.URL == a.BaseURL && f.OwnerUserID == a.OwnerUserID && f.WorkspaceID == a.WorkspaceID &&
		f.Provider == string(ProviderAutoposter) && f.SourceType == SourceAutoposterAPI
}

func referenceKey(feed Feed) operations.ReferenceCheckpointKey {
	return operations.ReferenceCheckpointKey{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID, FeedID: feed.ID}
}

// ConfigureAutoposter registers only explicitly supplied, valid server settings.
// It does not perform a network read or claim the token has been verified.
func (r *Registry) ConfigureAutoposter(ctx context.Context) (Feed, error) {
	a := r.opts.Autoposter
	if a == nil {
		return Feed{}, fmt.Errorf("accountfeed: Autoposter is not configured")
	}
	return r.RegisterContext(ctx, Feed{ID: a.feedID(), Name: "Secondhand Autoposter (reference only)",
		Provider: string(ProviderAutoposter), SourceType: SourceAutoposterAPI,
		URL: a.BaseURL, OwnerUserID: a.OwnerUserID, WorkspaceID: a.WorkspaceID,
		AccountLabel: a.RemoteOwner, OperationType: "read_only_reference", Enabled: false})
}

func (a AutoposterConnection) get(ctx context.Context, client *http.Client, path, cursor string) ([]byte, error) {
	u, _ := url.Parse(strings.TrimRight(a.BaseURL, "/") + path)
	if path == "/api/hai/records" {
		q := u.Query()
		q.Set("limit", "100")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("accountfeed: invalid Autoposter request")
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("accountfeed: Autoposter request failed (check endpoint, TLS, and connectivity)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("accountfeed: Autoposter HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("accountfeed: Autoposter response read failed")
	}
	if len(data) > maxFeedBytes {
		return nil, fmt.Errorf("accountfeed: Autoposter page exceeds 5 MiB")
	}
	return data, nil
}

type autoposterRecord struct {
	ChangeID  string         `json:"change_id"`
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	SourceURL string         `json:"source_url"`
	UpdatedAt string         `json:"updated_at"`
	Deleted   bool           `json:"deleted"`
	Metadata  map[string]any `json:"metadata"`
}

type autoposterPage struct {
	Connector  string             `json:"connector"`
	ReadOnly   bool               `json:"read_only"`
	Records    []autoposterRecord `json:"records"`
	NextCursor *string            `json:"next_cursor"`
	HasMore    *bool              `json:"has_more"`
}

var listingIDPattern = regexp.MustCompile(`^listing:[1-9][0-9]{0,18}$`)
var cursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (record autoposterRecord) input(feed Feed) (operations.NewOperationInput, error) {
	sequence, err := strconv.ParseUint(record.ChangeID, 10, 63)
	if err != nil || sequence == 0 || strconv.FormatUint(sequence, 10) != record.ChangeID {
		return operations.NewOperationInput{}, fmt.Errorf("accountfeed: valid Autoposter change_id required")
	}
	if !listingIDPattern.MatchString(record.ID) || record.Type != "secondhand_listing" {
		return operations.NewOperationInput{}, fmt.Errorf("accountfeed: invalid Autoposter record identity/type")
	}
	updated, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if err != nil {
		// SQLite-backed Autoposter serializes naive UTC database timestamps.
		updated, err = time.Parse("2006-01-02T15:04:05.999999999", record.UpdatedAt)
	}
	if err != nil {
		return operations.NewOperationInput{}, fmt.Errorf("accountfeed: invalid Autoposter timestamp")
	}
	if record.Deleted {
		// A tombstone cannot smuggle old/private fields into retained evidence.
		record.Title = "Deleted " + record.ID
		record.Content, record.SourceURL = "", ""
		record.Metadata = nil
	}
	// Normalize authority before checking the metadata byte limit.
	if record.Metadata == nil {
		record.Metadata = map[string]any{}
	}
	record.Metadata["execution_authority"] = false
	record.Metadata["authority"] = "owner_record"
	item := GenericItem{ExternalID: record.ID, Title: record.Title, Content: record.Content,
		Provider: string(ProviderAutoposter), ItemType: string(ItemDocument), Metadata: record.Metadata}
	if err := item.Validate(0, 0); err != nil {
		return operations.NewOperationInput{}, fmt.Errorf("accountfeed: invalid or oversized Autoposter record")
	}
	if len(record.Title) > 4000 || len(record.SourceURL) > 4096 {
		return operations.NewOperationInput{}, fmt.Errorf("accountfeed: oversized Autoposter title/link")
	}
	if record.SourceURL != "" {
		u, err := url.Parse(record.SourceURL)
		if err != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || sourceURISecret.MatchString(record.SourceURL) {
			return operations.NewOperationInput{}, fmt.Errorf("accountfeed: unsafe Autoposter source link")
		}
	}
	// Importing this evidence never grants execution authority, regardless of
	// any source metadata. Only known fields survive normalization.
	evidence, _ := json.Marshal(record)
	identity, _ := json.Marshal([]string{feed.ID.String(), record.ID})
	return operations.NewOperationInput{
		OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID,
		Title: firstNonEmpty(record.Title, record.ID), Description: record.Content,
		OperationType: "read_only_reference", SourceType: string(SourceAutoposterAPI),
		SourceURI: record.SourceURL, SourceReceivedAt: &updated,
		SourceProvider: string(ProviderAutoposter), SourceAccount: feed.AccountLabel, SourceExternalID: record.ID,
		SourceRevisionHash: idempotency.SourceRevisionHash(string(evidence), ""),
		SourceSequence:     sequence,
		AccountFeedID:      &feed.ID, DedupeKey: string(identity), EvidenceJSON: string(evidence),
	}, nil
}

func parseAutoposterPage(data []byte, feed Feed, cursor string) (autoposterPage, []operations.NewOperationInput, error) {
	var page autoposterPage
	if err := json.Unmarshal(data, &page); err != nil || page.Connector != "secondhand-platforms-autoposter" || !page.ReadOnly || page.Records == nil || page.HasMore == nil || len(page.Records) > 100 {
		return page, nil, fmt.Errorf("accountfeed: invalid Autoposter page contract")
	}
	next := ""
	if page.NextCursor != nil {
		next = *page.NextCursor
	}
	if (next != "" && !cursorPattern.MatchString(next)) || (len(page.Records) > 0 && (next == "" || next == cursor)) || (*page.HasMore && len(page.Records) == 0) || (len(page.Records) == 0 && next != cursor) {
		return page, nil, fmt.Errorf("accountfeed: invalid or non-advancing Autoposter cursor")
	}
	inputs := make([]operations.NewOperationInput, 0, len(page.Records))
	for _, record := range page.Records {
		in, err := record.input(feed)
		if err != nil {
			return page, nil, err
		}
		if len(inputs) > 0 && inputs[len(inputs)-1].SourceSequence >= in.SourceSequence {
			return page, nil, fmt.Errorf("accountfeed: Autoposter page is not in change order")
		}
		inputs = append(inputs, in)
	}
	return page, inputs, nil
}

func (r *Registry) syncAutoposter(ctx context.Context, feed Feed, controlRevision uint64) SyncReport {
	rep := SyncReport{FeedID: feed.ID.String()}
	fail := func(message string) SyncReport {
		rep.Errors = append(rep.Errors, message)
		return rep
	}
	a := r.opts.Autoposter
	if a == nil || !a.matches(feed) {
		return fail("accountfeed: Autoposter binding unavailable")
	}
	if err := a.validate(); err != nil {
		return fail(err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	key := referenceKey(feed)
	control, err := r.ops.GetReferenceSourceControl(ctx, key)
	if err != nil {
		return fail("accountfeed: Autoposter source control unavailable")
	}
	if !control.Enabled {
		return fail("accountfeed: Autoposter source disabled")
	}
	if control.Revision != controlRevision {
		return fail("accountfeed: Autoposter source control changed while waiting; start a new sync")
	}
	checkControl := func() error {
		current, err := r.ops.GetReferenceSourceControl(ctx, key)
		if err != nil {
			return fmt.Errorf("accountfeed: Autoposter source control unavailable")
		}
		if !current.Enabled {
			return fmt.Errorf("accountfeed: Autoposter source disabled")
		}
		if current.Revision != control.Revision {
			return fmt.Errorf("accountfeed: Autoposter source control changed; start a new sync")
		}
		return nil
	}
	// Reuse the connection within a sync, never redirect bearer credentials or
	// send them through an inherited proxy. Default TLS verification is retained.
	allowLoopback := false
	if parsed, parseErr := url.Parse(a.BaseURL); parseErr == nil && parsed.Scheme == "http" {
		allowLoopback = true
	}
	client := safeFeedHTTPClient(allowLoopback)
	transport := client.Transport.(*http.Transport)
	defer transport.CloseIdleConnections()
	data, err := a.get(ctx, client, "/api/hai/status", "")
	if err != nil {
		return fail(err.Error())
	}
	var status struct {
		Status    string `json:"status"`
		Connector string `json:"connector"`
		Owner     string `json:"owner"`
		Mode      string `json:"mode"`
		WriteBack *bool  `json:"write_back"`
	}
	if err := json.Unmarshal(data, &status); err != nil || status.Status != "ok" || status.Connector != "secondhand-platforms-autoposter" || status.Owner != a.RemoteOwner || status.Mode != "read_only_pull" || status.WriteBack == nil || *status.WriteBack {
		return fail("accountfeed: Autoposter owner/read-only status mismatch")
	}
	rep.ReadCompleted = true
	checkpoint, err := r.ops.LoadReferenceCheckpoint(ctx, key)
	if err != nil {
		return fail("accountfeed: Autoposter checkpoint unavailable; check database migrations and source state")
	}
	rep.Cursor = checkpoint.Cursor
	seen := map[string]bool{rep.Cursor: true}
	for pageN := 0; pageN < 10; pageN++ {
		if err := checkControl(); err != nil {
			return fail(err.Error())
		}
		data, err := a.get(ctx, client, "/api/hai/records", rep.Cursor)
		if err != nil {
			return fail(err.Error())
		}
		page, inputs, err := parseAutoposterPage(data, feed, rep.Cursor)
		if err != nil {
			return fail(err.Error())
		}
		if len(inputs) > 0 && seen[*page.NextCursor] {
			return fail("accountfeed: repeated Autoposter cursor")
		}
		if len(inputs) > 0 && inputs[0].SourceSequence <= checkpoint.Sequence {
			return fail("accountfeed: Autoposter change sequence did not advance; source restore/reset requires operator review")
		}
		for i, in := range inputs {
			if r.privacy != nil {
				scan := r.privacy.Scan(in.Description, page.Records[i].ID, "", 280)
				if !scan.Result.SafeForCloudModel {
					rep.PrivacyFlagged++
				}
			}
		}
		if len(inputs) > 0 {
			results, err := r.ops.SyncReferencePage(ctx, key, control.Revision, checkpoint, *page.NextCursor, inputs)
			if err != nil {
				if errors.Is(err, operations.ErrReferenceSourceDisabled) || errors.Is(err, operations.ErrReferenceSourceChanged) {
					return fail("accountfeed: Autoposter source disabled or control changed; page was not committed")
				}
				if errors.Is(err, operations.ErrReferenceCheckpointConflict) {
					return fail("accountfeed: another sync advanced this source; retry to resume its checkpoint")
				}
				return fail("accountfeed: Autoposter page commit could not be confirmed; retry from the stored checkpoint")
			}
			for i, res := range results {
				rep.ItemsRead++
				if res.Created {
					rep.OperationsCreated++
				} else {
					rep.OperationsRefresh++
				}
				if page.Records[i].Deleted {
					rep.TombstonesApplied++
				}
			}
			checkpoint = operations.ReferenceCheckpoint{Cursor: *page.NextCursor, Sequence: inputs[len(inputs)-1].SourceSequence}
		}
		rep.Cursor = checkpoint.Cursor
		if err := checkControl(); err != nil {
			return fail(err.Error())
		}
		seen[rep.Cursor] = true
		rep.HasMore = *page.HasMore
		if !rep.HasMore {
			break
		}
	}
	return rep
}

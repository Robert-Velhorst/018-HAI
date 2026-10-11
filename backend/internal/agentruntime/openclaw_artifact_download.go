package agentruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const OpenClawArtifactContentLimit = 8 << 20
const openClawArtifactDownloadFrameLimit = (OpenClawArtifactContentLimit+2)/3*4 + (64 << 10)

var ErrOpenClawArtifactURL = errors.New("OpenClaw artifact download origin is not authorized")
var ErrOpenClawArtifactUnsupported = errors.New("OpenClaw artifact content is unsupported or exceeds the download limit")

func validateOpenClawArtifactJSON(payload []byte) error {
	if len(payload) == 0 || !utf8.Valid(payload) {
		return fmt.Errorf("invalid artifact JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	nodes := 0
	if err := consumeOpenClawArtifactJSONValue(decoder, 0, &nodes); err != nil {
		return fmt.Errorf("invalid artifact JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("invalid artifact JSON")
	}
	return nil
}

func consumeOpenClawArtifactJSONValue(decoder *json.Decoder, depth int, nodes *int) error {
	*nodes = *nodes + 1
	if depth > 64 || *nodes > 200000 {
		return fmt.Errorf("artifact JSON structure exceeds limits")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid artifact JSON object key")
			}
			foldedKey := foldOpenClawArtifactJSONKey(key)
			if _, duplicate := seen[foldedKey]; duplicate {
				return fmt.Errorf("duplicate artifact JSON field")
			}
			seen[foldedKey] = struct{}{}
			if err := consumeOpenClawArtifactJSONValue(decoder, depth+1, nodes); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("invalid artifact JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeOpenClawArtifactJSONValue(decoder, depth+1, nodes); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return fmt.Errorf("invalid artifact JSON array")
		}
	default:
		return fmt.Errorf("invalid artifact JSON delimiter")
	}
	return nil
}

// encoding/json accepts case-folded field names when decoding into structs.
// Reject keys that it would treat as the same field, not just identical JSON
// spellings, so validation and struct decoding cannot disagree.
func foldOpenClawArtifactJSONKey(key string) string {
	var folded strings.Builder
	folded.Grow(len(key))
	for _, character := range key {
		if character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		} else {
			for {
				next := unicode.SimpleFold(character)
				if next <= character {
					character = next
					break
				}
				character = next
			}
		}
		folded.WriteRune(character)
	}
	return folded.String()
}

// Content is delivered only through the owner-scoped attachment endpoint. It
// must never be embedded in general runtime/audit JSON or treated as verified work.
type GatewayArtifactContent struct {
	Data          []byte `json:"-"`
	ContentSHA256 string `json:"contentSHA256"`
	descriptor    GatewayArtifactDescriptor
}

type openClawArtifactSummary struct {
	ID         string `json:"id"`
	SessionKey string `json:"sessionKey"`
	Download   struct {
		Mode string `json:"mode"`
	} `json:"download"`
}

func descriptorForOpenClawArtifact(raw json.RawMessage, runID string) (GatewayArtifactDescriptor, error) {
	if len(raw) > openClawArtifactDownloadFrameLimit || validateOpenClawArtifactJSON(raw) != nil {
		return GatewayArtifactDescriptor{}, fmt.Errorf("invalid artifact metadata")
	}
	wrapped := append([]byte(`{"artifacts":[`), raw...)
	wrapped = append(wrapped, ']', '}')
	list, err := openClawGatewayArtifactDescriptorsFromResponse(wrapped, runID)
	if err != nil || len(list) != 1 {
		return GatewayArtifactDescriptor{}, fmt.Errorf("invalid artifact metadata")
	}
	return list[0], nil
}

func sameOpenClawArtifactSize(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func parseOpenClawArtifactDownload(payload json.RawMessage, id string, receipt OpenClawGatewayReceipt) (*GatewayArtifactContent, error) {
	if len(payload) > openClawArtifactDownloadFrameLimit || validateOpenClawArtifactJSON(payload) != nil {
		return nil, ErrOpenClawArtifactUnsupported
	}
	var response struct {
		Artifact  json.RawMessage `json:"artifact"`
		Encoding  string          `json:"encoding"`
		Data      *string         `json:"data"`
		URL       *string         `json:"url"`
		ExpiresAt string          `json:"expiresAt"`
	}
	if json.Unmarshal(payload, &response) != nil {
		return nil, fmt.Errorf("invalid artifact download")
	}
	descriptor, err := descriptorForOpenClawArtifact(response.Artifact, receipt.RunID)
	var summary openClawArtifactSummary
	if err != nil || json.Unmarshal(response.Artifact, &summary) != nil || summary.ID != id || (summary.SessionKey != "" && summary.SessionKey != receipt.SessionKey) {
		return nil, fmt.Errorf("artifact download scope mismatch")
	}
	if descriptor.SizeBytes != nil && (*descriptor.SizeBytes < 0 || *descriptor.SizeBytes > OpenClawArtifactContentLimit) {
		return nil, ErrOpenClawArtifactUnsupported
	}
	if summary.Download.Mode == "url" && response.Data == nil && response.Encoding == "" && response.URL != nil && *response.URL != "" {
		if !artifactURLExpiryValid(response.ExpiresAt, time.Now().UTC()) {
			return nil, fmt.Errorf("artifact download URL expired or invalid")
		}
		return nil, &openClawArtifactURLResult{url: *response.URL, expiresAt: response.ExpiresAt, descriptor: descriptor}
	}
	if summary.Download.Mode == "unsupported" {
		return nil, ErrOpenClawArtifactUnsupported
	}
	if summary.Download.Mode != "bytes" || response.Encoding != "base64" || response.Data == nil || response.URL != nil {
		return nil, fmt.Errorf("invalid artifact content encoding")
	}
	if len(*response.Data) > base64.StdEncoding.EncodedLen(OpenClawArtifactContentLimit) {
		return nil, ErrOpenClawArtifactUnsupported
	}
	data, err := base64.StdEncoding.Strict().DecodeString(*response.Data)
	if err != nil {
		return nil, fmt.Errorf("artifact content size or encoding mismatch")
	}
	if len(data) > OpenClawArtifactContentLimit {
		return nil, ErrOpenClawArtifactUnsupported
	}
	if descriptor.SizeBytes != nil && *descriptor.SizeBytes != int64(len(data)) {
		return nil, fmt.Errorf("artifact content size or encoding mismatch")
	}
	sum := sha256.Sum256(data)
	return &GatewayArtifactContent{Data: data, ContentSHA256: hex.EncodeToString(sum[:]), descriptor: descriptor}, nil
}

// Resolve the one-way stored identifier against native metadata, then request
// exactly that artifact in exactly that run. URL responses use only the separate
// approved-origin transport; no shell or local file path is used.
func (r *Registry) DownloadOpenClawGatewayArtifact(ctx context.Context, expected OpenClawGatewayReceipt, descriptor GatewayArtifactDescriptor) (*GatewayArtifactContent, error) {
	if !r.OpenClawArtifactRecoveryReady() || expected.OwnerIdentity == "" {
		return nil, fmt.Errorf("OpenClaw artifact download unavailable")
	}
	if digest, err := hex.DecodeString(descriptor.Digest); err != nil || len(digest) != 32 || descriptor.Digest != strings.ToLower(descriptor.Digest) {
		return nil, fmt.Errorf("invalid artifact reference")
	}
	a := r.adapters["openclaw"].(*openClawAdapter)
	receipt, err := a.openClawGatewayReceiptForReference(expected.RuntimeTaskID, expected.ExecutionReference)
	if err != nil || receipt.OwnerIdentity != expected.OwnerIdentity || receipt.RunID != expected.RunID || !validOpenClawGatewayRunID(receipt.RunID) || receipt.SessionKey != expected.SessionKey || receipt.SessionID != expected.SessionID || receipt.TerminalStatus != "completed" || receipt.TerminalAt.IsZero() || !receipt.TerminalAt.Equal(expected.TerminalAt) {
		return nil, fmt.Errorf("artifact receipt mismatch")
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	connection, methods, err := a.openClawGatewayOperatorReadConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("OpenClaw artifact download could not connect")
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	if !containsExact(methods, "artifacts.list") || !containsExact(methods, "artifacts.download") {
		return nil, fmt.Errorf("OpenClaw artifact download capability unavailable")
	}
	payload, err := openClawGatewayReadOnlyRequestWithRetry(ctx, connection, "artifacts.list", map[string]any{"runId": receipt.RunID})
	if err != nil {
		return nil, fmt.Errorf("OpenClaw artifact lookup unavailable")
	}
	if len(payload) > defaultOutputLimit || validateOpenClawArtifactJSON(payload) != nil {
		return nil, fmt.Errorf("invalid OpenClaw artifact list")
	}
	descriptors, err := openClawGatewayArtifactDescriptorsFromResponse(payload, receipt.RunID)
	var list struct {
		Artifacts []json.RawMessage `json:"artifacts"`
	}
	if err != nil || json.Unmarshal(payload, &list) != nil {
		return nil, fmt.Errorf("invalid OpenClaw artifact list")
	}
	for i, current := range descriptors {
		if current.Digest != descriptor.Digest {
			continue
		}
		if current.Type != descriptor.Type || current.MIMEType != descriptor.MIMEType || !sameOpenClawArtifactSize(current.SizeBytes, descriptor.SizeBytes) {
			return nil, fmt.Errorf("artifact metadata changed since collection")
		}
		var summary openClawArtifactSummary
		if json.Unmarshal(list.Artifacts[i], &summary) != nil || (summary.SessionKey != "" && summary.SessionKey != receipt.SessionKey) {
			return nil, fmt.Errorf("artifact lookup scope mismatch")
		}
		if current.SizeBytes != nil && *current.SizeBytes > OpenClawArtifactContentLimit {
			return nil, ErrOpenClawArtifactUnsupported
		}
		// Only this dedicated connection admits a bounded content frame. Metadata
		// reads on every other connection retain their original 64 KiB frame limit.
		connection.MaxPayloadBytes = openClawArtifactDownloadFrameLimit
		if !r.OpenClawArtifactRecoveryReady() || ctx.Err() != nil {
			return nil, fmt.Errorf("OpenClaw artifact download paused or cancelled")
		}
		payload, err = openClawGatewayReadOnlyRequest(connection, "artifacts.download", map[string]any{"runId": receipt.RunID, "artifactId": summary.ID})
		if err != nil {
			return nil, fmt.Errorf("OpenClaw artifact content unavailable")
		}
		content, err := parseOpenClawArtifactDownload(payload, summary.ID, receipt)
		if err == nil && (content.descriptor.Type != current.Type || content.descriptor.MIMEType != current.MIMEType || !sameOpenClawArtifactSize(content.descriptor.SizeBytes, current.SizeBytes)) {
			return nil, fmt.Errorf("artifact download metadata changed")
		}
		var downloadURL *openClawArtifactURLResult
		if errors.As(err, &downloadURL) {
			if downloadURL.descriptor.Type != current.Type || downloadURL.descriptor.MIMEType != current.MIMEType || !sameOpenClawArtifactSize(downloadURL.descriptor.SizeBytes, current.SizeBytes) {
				return nil, fmt.Errorf("artifact download metadata changed")
			}
			if !r.OpenClawArtifactRecoveryReady() || ctx.Err() != nil || !artifactURLExpiryValid(downloadURL.expiresAt, time.Now().UTC()) {
				return nil, fmt.Errorf("artifact URL download paused, cancelled or expired")
			}
			downloadContext := ctx
			if downloadURL.expiresAt != "" {
				expiry, _ := time.Parse(time.RFC3339, downloadURL.expiresAt)
				var stopDownload context.CancelFunc
				downloadContext, stopDownload = context.WithDeadline(ctx, expiry)
				defer stopDownload()
			}
			content, err = fetchOpenClawArtifactURL(downloadContext, downloadURL.url, a.artifactDownloadOrigins, downloadURL.descriptor.SizeBytes)
		}
		if err == nil && current.SizeBytes != nil && *current.SizeBytes != int64(len(content.Data)) {
			return nil, fmt.Errorf("artifact content changed since lookup")
		}
		return content, err
	}
	return nil, fmt.Errorf("stored artifact is no longer available in this run")
}

package safety

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxRedactionBytes = 1 << 20
	maxRedactionDepth = 64
)

var (
	secretKeyPattern         = regexp.MustCompile(`"(?:\\.|[^"\\])*"\s*[:=]\s*|'(?:\\.|[^'\\])*'\s*[:=]\s*`)
	quotedTextPattern        = regexp.MustCompile(`"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'`)
	absoluteURLShapePattern  = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
	unquotedSecretKeyPattern = regexp.MustCompile(`(?i:\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|authorization|private[_-]?key|passphrase|credentials?|encryption[_-]?key)\b)\s*[:=]\s*`)
	bearerSecretPattern      = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`)
	cookieHeaderPattern      = regexp.MustCompile(`(?im)(^|[ \t])((?:cookie|set-cookie)\s*[:=]\s*)[^\r\n]*`)
	providerTokenPattern     = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]{20,}|gh[pousr]_[a-z0-9_]{20,}|github_pat_[a-z0-9_]{20,}|xox[baprs]-[a-z0-9-]{20,})\b`)
	privateKeyPattern        = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)
)

func RedactSecrets(value string) string {
	if value == "" {
		return ""
	}
	// Oversize output is discarded, never truncated before secret detection.
	if len(value) > maxRedactionBytes {
		return "[REDACTED_SIZE_LIMIT]"
	}
	if json.Valid([]byte(value)) {
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if structured, changed, err := redactJSON(decoder, 0); err == nil {
			if !changed {
				return value
			}
			encoded, err := json.Marshal(structured)
			if err == nil && len(encoded) <= maxRedactionBytes {
				return string(encoded)
			}
			if err == nil {
				return "[REDACTED_SIZE_LIMIT]"
			}
		}
		return "[REDACTED_JSON_ERROR]"
	}
	return redactText(value)
}

// Visit every occurrence before duplicate object keys can overwrite values.
// Only actual sanitization permits reserialization of the original bytes.
func redactJSON(decoder *json.Decoder, depth int) (any, bool, error) {
	if depth >= maxRedactionDepth {
		var discarded any
		err := decoder.Decode(&discarded)
		return "[REDACTED_DEPTH_LIMIT]", true, err
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, false, err
	}
	switch value := token.(type) {
	case json.Delim:
		changed := false
		if value == '{' {
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, false, err
				}
				key := keyToken.(string)
				safeKey := redactText(key)
				if sensitiveKey(key) || safeKey != key {
					var child any
					if err := decoder.Decode(&child); err != nil {
						return nil, false, err
					}
					text, isString := child.(string)
					changed = changed || safeKey != key || !isString || text != "[REDACTED]"
					object[safeKey] = "[REDACTED]"
				} else {
					child, childChanged, err := redactJSON(decoder, depth+1)
					if err != nil {
						return nil, false, err
					}
					object[key] = child
					changed = changed || childChanged
				}
			}
			_, err := decoder.Token()
			return object, changed, err
		}
		array := make([]any, 0)
		for decoder.More() {
			child, childChanged, err := redactJSON(decoder, depth+1)
			if err != nil {
				return nil, false, err
			}
			array = append(array, child)
			changed = changed || childChanged
		}
		_, err := decoder.Token()
		return array, changed, err
	case string:
		redacted := redactText(value)
		return redacted, redacted != value, nil
	default:
		return value, false, nil
	}
}

func redactText(value string) string {
	value = redactAbsoluteURL(value)
	redacted := redactQuotedURLs(value)
	redacted = privateKeyPattern.ReplaceAllString(redacted, "[REDACTED_PRIVATE_KEY]")
	// Cookie values can contain several credentials separated by semicolons.
	redacted = cookieHeaderPattern.ReplaceAllString(redacted, "${1}${2}[REDACTED]")
	redacted = bearerSecretPattern.ReplaceAllString(redacted, "Bearer [REDACTED]")
	redacted = providerTokenPattern.ReplaceAllString(redacted, "[REDACTED_PROVIDER_TOKEN]")
	redacted = redactAssignments(redacted, secretKeyPattern, false)
	return redactAssignments(redacted, unquotedSecretKeyPattern, true)
}

func redactQuotedURLs(value string) string {
	return quotedTextPattern.ReplaceAllStringFunc(value, func(quoted string) string {
		raw := quoted[1 : len(quoted)-1]
		if quoted[0] == '"' {
			if err := json.Unmarshal([]byte(quoted), &raw); err != nil {
				decoded, decodeErr := strconv.Unquote(quoted)
				if decodeErr != nil {
					if absoluteURLShapePattern.MatchString(strings.TrimSpace(raw)) {
						return `"[REDACTED_URL_ERROR]"`
					}
					return quoted
				}
				raw = decoded
			}
		}
		sanitized := redactAbsoluteURL(raw)
		if sanitized == raw {
			return quoted
		}
		encoded, err := json.Marshal(sanitized)
		if err != nil {
			return `"[REDACTED_URL_ERROR]"`
		}
		return string(encoded)
	})
}

func redactAbsoluteURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		if absoluteURLShapePattern.MatchString(strings.TrimSpace(raw)) {
			return "[REDACTED_URL_ERROR]"
		}
		return raw
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "[REDACTED_URL_ERROR]"
	}
	changed := parsed.User != nil
	for key, values := range query {
		if sensitiveKey(key) {
			for _, entry := range values {
				changed = changed || strings.TrimSpace(entry) != "[REDACTED]"
			}
		}
	}
	if !changed {
		return raw
	}
	return RedactURL(raw)
}

func redactAssignments(redacted string, pattern *regexp.Regexp, unquoted bool) string {
	var output strings.Builder
	last := 0
	for _, match := range pattern.FindAllStringIndex(redacted, -1) {
		if match[0] < last {
			continue
		}
		prefix := redacted[match[0]:match[1]]
		// The separator is last: quoted keys may themselves contain ':' or '='.
		separator := strings.LastIndexAny(prefix, ":=")
		key := strings.TrimSpace(prefix[:separator])
		if key[0] == '"' {
			if err := json.Unmarshal([]byte(key), &key); err != nil {
				continue
			}
		} else if key[0] == '\'' {
			key = key[1 : len(key)-1]
		}
		// A Go HTTP error quotes its URL before ': dial ...'. It is not a
		// field name; the separate bare-assignment pass handles its query.
		if !unquoted {
			if parsed, err := url.Parse(key); err == nil && parsed.Scheme != "" && parsed.Host != "" {
				continue
			}
		}
		if !sensitiveKey(key) {
			continue
		}
		end := secretValueEnd(redacted, match[1])
		output.WriteString(redacted[last:match[1]])
		original := redacted[match[1]:end]
		alreadyRedacted := original == "[REDACTED]" || original == `"[REDACTED]"` || original == "'[REDACTED]'"
		if unquoted && !alreadyRedacted && !strings.ContainsRune(" \t\r\n", rune(redacted[match[1]-1])) {
			output.WriteByte(' ')
		}
		if match[1] < len(redacted) && redacted[match[1]] == '"' {
			output.WriteString(`"[REDACTED]"`)
		} else if match[1] < len(redacted) && redacted[match[1]] == '\'' {
			output.WriteString("'[REDACTED]'")
		} else {
			output.WriteString("[REDACTED]")
		}
		last = end
	}
	output.WriteString(redacted[last:])
	if output.Len() > maxRedactionBytes {
		return "[REDACTED_SIZE_LIMIT]"
	}
	return output.String()
}

// Malformed/truncated values have no trustworthy boundary; discard their tail.
func secretValueEnd(value string, start int) int {
	if start >= len(value) {
		return len(value)
	}
	if value[start] == '"' || value[start] == '\'' {
		return quotedValueEnd(value, start)
	}
	if value[start] == '{' || value[start] == '[' {
		stack := make([]byte, 0, maxRedactionDepth)
		for i := start; i < len(value); i++ {
			switch value[i] {
			case '"', '\'':
				i = quotedValueEnd(value, i) - 1
			case '{', '[':
				if len(stack) == maxRedactionDepth {
					return len(value)
				}
				stack = append(stack, value[i])
			case '}', ']':
				opening := stack[len(stack)-1]
				if (opening == '{' && value[i] != '}') || (opening == '[' && value[i] != ']') {
					return len(value)
				}
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return i + 1
				}
			}
		}
		return len(value)
	}
	for i := start; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if strings.ContainsRune(" \t\r\n,;}]\"'", rune(value[i])) {
			return i
		}
	}
	return len(value)
}

func quotedValueEnd(value string, start int) int {
	for i := start + 1; i < len(value); i++ {
		if value[i] == '\\' {
			i++
		} else if value[i] == value[start] {
			return i + 1
		}
	}
	return len(value)
}

func RedactURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return RedactSecrets(raw)
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if sensitiveKey(key) {
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// IsSensitiveKey exposes the same credential-field policy used by text and JSON redaction.
func IsSensitiveKey(key string) bool { return sensitiveKey(key) }

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	return strings.Contains(key, "password") ||
		strings.Contains(key, "passwd") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey") ||
		key == "pwd" ||
		key == "key" ||
		key == "cookie" ||
		key == "set_cookie" ||
		key == "setcookie" ||
		key == "private_key" || key == "privatekey" ||
		key == "passphrase" ||
		key == "credential" || key == "credentials" ||
		key == "encryption_key" || key == "encryptionkey" ||
		strings.Contains(key, "authorization")
}

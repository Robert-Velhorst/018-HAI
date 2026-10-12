package task

import (
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

// ReadOnlyRuntimeInspector reports configured action metadata, not permission
// to execute. Approval and every final execution check remain the launcher's job.
type ReadOnlyRuntimeInspector interface {
	IsReadOnlyRuntime(automationID string) (bool, error)
}

type readOnlyRuntimeReader interface {
	FindByID(uuid.UUID) (*models.Automation, error)
}

var _ ReadOnlyRuntimeInspector = (*AutomationToolExecutor)(nil)

func (e *AutomationToolExecutor) IsReadOnlyRuntime(automationID string) (bool, error) {
	id, err := uuid.Parse(strings.TrimSpace(automationID))
	if err != nil || id == uuid.Nil {
		return false, errors.New("automationId must identify a nonzero automation UUID")
	}
	if e == nil || e.launcher == nil {
		return false, nil
	}
	reader, ok := e.launcher.(readOnlyRuntimeReader)
	if !ok {
		return false, nil
	}
	// A typed nil can satisfy the optional reader interface without being usable.
	value := reflect.ValueOf(reader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return false, nil
		}
	}
	record, err := reader.FindByID(id)
	if err != nil {
		// Repository errors can embed credential-bearing URLs; do not wrap them.
		return false, errors.New("automation runtime metadata could not be read")
	}
	if record == nil || record.ID != id || record.LaunchType != "api" {
		return false, nil
	}

	// executeAPILaunch currently defaults methodless targets to POST, not GET.
	// Its explicit method parser uppercases the first whitespace-delimited token
	// and trims the remaining URL. Only its GET/HEAD subset is read-only metadata.
	configured := strings.TrimSpace(record.LaunchTarget)
	fields := strings.Fields(configured)
	if len(fields) < 2 {
		return false, nil
	}
	method := strings.ToUpper(fields[0])
	if method != http.MethodGet && method != http.MethodHead {
		return false, nil
	}
	target := strings.TrimSpace(strings.TrimPrefix(configured, fields[0]))
	for _, r := range target {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false, nil
		}
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return false, nil
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return false, nil
		}
	} else if strings.HasSuffix(parsed.Host, ":") {
		return false, nil
	}
	return true, nil
}

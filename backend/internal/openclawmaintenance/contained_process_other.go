//go:build !windows

package openclawmaintenance

import (
	"context"
	"errors"
)

func probeCompanionProcessContainment(context.Context) error {
	return errors.New("Windows Job Object containment is unavailable on this platform")
}

func runContainedCompanionProcess(context.Context, string, []string, []string) (containedProcessResult, error) {
	return containedProcessResult{}, errors.New("Windows Job Object containment is unavailable on this platform")
}

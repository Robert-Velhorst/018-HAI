package openclawmaintenance

import (
	"context"
	"errors"
	"fmt"
)

var ErrInstallerDescendantsRemain = errors.New("installer descendants remained after the primary process exited")
var ErrProcessTreeTerminationUnverified = errors.New("installer process-tree termination could not be verified")

type containedProcessResult struct {
	ExitCode              uint32
	TerminationStatus     string
	DescendantsTerminated bool
}

type ProcessTreeTerminationError struct {
	Status string
	cause  error
}

func (e *ProcessTreeTerminationError) Error() string {
	if e == nil || e.Status == "unknown" {
		return "Companion installer process-tree termination is unknown"
	}
	if errors.Is(e.cause, ErrInstallerDescendantsRemain) {
		return "Companion installer left descendant processes; they were stopped and the update requires review"
	}
	return "Companion installer did not complete successfully inside its contained process job"
}

func (e *ProcessTreeTerminationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func processTerminationStatus(err error) string {
	var terminationErr *ProcessTreeTerminationError
	if errors.As(err, &terminationErr) {
		return terminationErr.Status
	}
	return ""
}

func resumeContainedThreadIfActive(ctx context.Context, resume func() (uint32, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if resume == nil {
		return fmt.Errorf("contained process resume operation is unavailable")
	}
	suspendCount, err := resume()
	if err != nil || suspendCount != 1 {
		return fmt.Errorf("contained process could not be resumed")
	}
	return nil
}

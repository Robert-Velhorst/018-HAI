package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const processWaitDelay = 2 * time.Second

var (
	errProcessTreeTerminationUnverified = errors.New("contained process tree termination could not be verified")
	errOutputCaptureIncomplete          = errors.New("contained process output capture is incomplete")
	errOutputCaptureLimitExceeded       = errors.New("captured output exceeded the configured byte limit")
)

func copyProcessOutput(reader io.Reader, destination io.Writer) error {
	_, err := io.Copy(destination, reader)
	if limited, ok := destination.(*limitedBuffer); ok && limited.truncated {
		if err != nil {
			return errors.Join(errOutputCaptureLimitExceeded, err)
		}
		return errOutputCaptureLimitExceeded
	}
	return err
}

func outputCaptureError(stream string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: capture %s: %w", errOutputCaptureIncomplete, stream, err)
}

type processSpec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

type processResult struct {
	ExitCode              int
	Stdout                string
	Stderr                string
	processTreeTerminated bool
}

// suspendedProcess represents a process that cannot execute until Resume.
// Terminate must stop the entire contained process tree and return boundedly.
type suspendedProcess interface {
	Resume() error
	Wait(context.Context) (processResult, error)
	Terminate() error
	Close() error
}

type processStarter func(processSpec) (suspendedProcess, error)

// runContainedProcess revalidates while the child is suspended, then calls
// Resume locally. The validation and Resume are separate operations: polling
// cannot order this remote start atomically with server stop/clear. Production
// bridge entry points must remain fail-closed until an acknowledged protocol
// or equivalent OS-level gate closes that distributed gap.
func runContainedProcess(ctx context.Context, spec processSpec, revalidate func(context.Context) error, onStarted func(), start processStarter) (result processResult, retErr error) {
	if err := ctx.Err(); err != nil {
		return processResult{ExitCode: -1}, err
	}
	if revalidate == nil {
		return processResult{ExitCode: -1}, errors.New("process start requires a final authorization check")
	}
	if start == nil {
		return processResult{ExitCode: -1}, errors.New("process start supervisor is unavailable")
	}
	process, err := start(spec)
	if err != nil {
		return processResult{ExitCode: -1}, err
	}
	defer func() {
		if closeErr := process.Close(); closeErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("close contained process: %w", closeErr)
			} else {
				retErr = errors.Join(retErr, fmt.Errorf("close contained process: %w", closeErr))
			}
		}
	}()
	stop := func(cause error) (processResult, error) {
		result := processResult{ExitCode: -1}
		if terminateErr := process.Terminate(); terminateErr != nil {
			cause = errors.Join(cause, fmt.Errorf("%w: %v", errProcessTreeTerminationUnverified, terminateErr))
		} else {
			result.processTreeTerminated = true
		}
		return result, cause
	}
	if err := ctx.Err(); err != nil {
		return stop(err)
	}
	if err := revalidate(ctx); err != nil {
		return stop(err)
	}
	if err := ctx.Err(); err != nil {
		return stop(err)
	}
	if err := process.Resume(); err != nil {
		return stop(fmt.Errorf("resume contained process: %w", err))
	}
	if onStarted != nil {
		onStarted()
	}
	result, err = process.Wait(ctx)
	if err != nil {
		if terminateErr := process.Terminate(); terminateErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: %v", errProcessTreeTerminationUnverified, terminateErr))
		} else {
			result.processTreeTerminated = true
		}
		return result, err
	}
	if err := ctx.Err(); err != nil {
		if terminateErr := process.Terminate(); terminateErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: %v", errProcessTreeTerminationUnverified, terminateErr))
		} else {
			result.processTreeTerminated = true
		}
		return result, err
	}
	return result, nil
}

func isWSLBackedConfig(executable, workspace, stateDir string, envAllow []string) bool {
	// These checks catch explicit WSL commands, WSL UNC paths, and forwarded
	// WSL environment. They cannot inspect arbitrary native .exe wrapper logic
	// or DSH plugin behavior; a plugin that invokes wsl.exe is not blocked by
	// this detector, and its Linux task is not contained by the Windows Job.
	for _, value := range []string{executable, workspace, stateDir} {
		clean := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "/", `\`))
		if strings.Contains(clean, `\wsl$\`) || strings.Contains(clean, `\wsl.localhost\`) {
			return true
		}
	}
	normalizedExecutable := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(executable), "/", `\`))
	base := normalizedExecutable[strings.LastIndex(normalizedExecutable, `\`)+1:]
	if base == "wsl" || base == "wsl.exe" {
		return true
	}
	for _, key := range envAllow {
		key = strings.ToUpper(strings.TrimSpace(key))
		if key == "WSLENV" || strings.HasPrefix(key, "WSL_") {
			return true
		}
	}
	return false
}

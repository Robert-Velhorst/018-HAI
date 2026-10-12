//go:build windows

package openclawmaintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	containedProcessWaitInterval = 100 * time.Millisecond
	descendantGracePeriod        = 2 * time.Second
	processTreeStopTimeout       = 10 * time.Second
)

type jobBasicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func probeCompanionProcessContainment(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	powershell, err := trustedMaintenanceCommandPath("powershell.exe")
	if err != nil {
		return err
	}
	probeContext, cancel := context.WithTimeout(parent, 500*time.Millisecond)
	defer cancel()
	_, runErr := runContainedCompanionProcess(probeContext, powershell, []string{
		"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30",
	}, commandEnvironment(os.Environ(), nil))
	if runErr == nil || processTerminationStatus(runErr) != "verified" ||
		(!errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled)) {
		return fmt.Errorf("Windows Job Object containment probe did not verify bounded cancellation")
	}
	return nil
}

func runContainedCompanionProcess(ctx context.Context, executable string, args, environment []string) (result containedProcessResult, runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !filepath.IsAbs(executable) || strings.TrimSpace(executable) != executable {
		return result, fmt.Errorf("contained process executable must be an absolute path")
	}
	appName, err := windows.UTF16PtrFromString(filepath.Clean(executable))
	if err != nil {
		return result, fmt.Errorf("contained process executable is invalid")
	}
	commandLine := windows.ComposeCommandLine(append([]string{executable}, args...))
	commandLineUTF16, err := windows.UTF16FromString(commandLine)
	if err != nil {
		return result, fmt.Errorf("contained process command line is invalid")
	}
	environmentBlock, err := windowsEnvironmentBlock(environment)
	if err != nil {
		return result, fmt.Errorf("contained process environment is invalid")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return result, fmt.Errorf("Windows Job Object could not be created")
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		return result, fmt.Errorf("Windows Job Object kill-on-close could not be enabled")
	}

	var startup windows.StartupInfo
	startup.Cb = uint32(unsafe.Sizeof(startup))
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(
		appName,
		&commandLineUTF16[0],
		nil,
		nil,
		false,
		flags,
		&environmentBlock[0],
		nil,
		&startup,
		&process,
	); err != nil {
		return result, fmt.Errorf("contained process could not be created suspended")
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)

	if err := windows.AssignProcessToJobObject(job, process.Process); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		if waitHandle(process.Process, processTreeStopTimeout) {
			result.TerminationStatus = "verified"
		}
		return result, &ProcessTreeTerminationError{Status: result.TerminationStatus, cause: fmt.Errorf("suspended process could not be assigned to the Job Object")}
	}
	if err := resumeContainedThreadIfActive(ctx, func() (uint32, error) {
		return windows.ResumeThread(process.Thread)
	}); err != nil {
		return terminateContainedProcess(job, process.Process, result, err)
	}

	if !waitProcessOrContext(ctx, process.Process) {
		return terminateContainedProcess(job, process.Process, result, ctx.Err())
	}
	if err := windows.GetExitCodeProcess(process.Process, &result.ExitCode); err != nil {
		return terminateContainedProcess(job, process.Process, result, fmt.Errorf("contained process exit status could not be read"))
	}
	if ctx.Err() != nil {
		return terminateContainedProcess(job, process.Process, result, ctx.Err())
	}

	graceContext, cancelGrace := context.WithTimeout(context.Background(), descendantGracePeriod)
	jobEmpty, queryErr := waitForJobEmpty(graceContext, job)
	cancelGrace()
	if queryErr != nil {
		return terminateContainedProcess(job, process.Process, result, queryErr)
	}
	if !jobEmpty {
		result, runErr = terminateContainedProcess(job, process.Process, result, ErrInstallerDescendantsRemain)
		if runErr != nil {
			return result, runErr
		}
		return result, &ProcessTreeTerminationError{Status: "verified", cause: ErrInstallerDescendantsRemain}
	}
	result.TerminationStatus = "verified"
	if result.ExitCode != 0 {
		return result, &ProcessTreeTerminationError{Status: "verified", cause: fmt.Errorf("contained process returned a nonzero exit status")}
	}
	return result, nil
}

func terminateContainedProcess(job, process windows.Handle, result containedProcessResult, cause error) (containedProcessResult, error) {
	_ = windows.TerminateJobObject(job, 1)
	stopContext, cancel := context.WithTimeout(context.Background(), processTreeStopTimeout)
	defer cancel()
	if !waitHandleContext(stopContext, process) {
		return result, &ProcessTreeTerminationError{Status: "unknown", cause: errors.Join(ErrProcessTreeTerminationUnverified, cause)}
	}
	empty, err := waitForJobEmpty(stopContext, job)
	if err != nil || !empty {
		return result, &ProcessTreeTerminationError{Status: "unknown", cause: errors.Join(ErrProcessTreeTerminationUnverified, cause, err)}
	}
	result.TerminationStatus = "verified"
	if errors.Is(cause, ErrInstallerDescendantsRemain) {
		result.DescendantsTerminated = true
		return result, &ProcessTreeTerminationError{Status: "verified", cause: cause}
	}
	return result, &ProcessTreeTerminationError{Status: "verified", cause: cause}
}

func waitProcessOrContext(ctx context.Context, process windows.Handle) bool {
	for {
		if waitHandle(process, containedProcessWaitInterval) {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
	}
}

func waitHandle(process windows.Handle, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return waitHandleContext(ctx, process)
}

func waitHandleContext(ctx context.Context, handle windows.Handle) bool {
	for {
		milliseconds := uint32(containedProcessWaitInterval / time.Millisecond)
		result, err := windows.WaitForSingleObject(handle, milliseconds)
		if err == nil && result == windows.WAIT_OBJECT_0 {
			return true
		}
		if err != nil || result == windows.WAIT_FAILED {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		default:
		}
	}
}

func waitForJobEmpty(ctx context.Context, job windows.Handle) (bool, error) {
	for {
		var accounting jobBasicAccountingInformation
		if err := windows.QueryInformationJobObject(
			job,
			windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&accounting)),
			uint32(unsafe.Sizeof(accounting)),
			nil,
		); err != nil {
			return false, fmt.Errorf("Windows Job Object process count could not be verified")
		}
		if accounting.ActiveProcesses == 0 {
			return true, nil
		}
		timer := time.NewTimer(containedProcessWaitInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return false, nil
		case <-timer.C:
		}
	}
}

func windowsEnvironmentBlock(environment []string) ([]uint16, error) {
	entries := append([]string(nil), environment...)
	for _, entry := range entries {
		if strings.ContainsAny(entry, "\x00\r\n") {
			return nil, fmt.Errorf("invalid environment entry")
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToUpper(entries[i]) < strings.ToUpper(entries[j])
	})
	text := strings.Join(entries, "\x00") + "\x00\x00"
	return utf16.Encode([]rune(text)), nil
}

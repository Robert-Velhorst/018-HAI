//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	processWaitInterval              = 20 * time.Millisecond
	processTerminationWait           = 2 * time.Second
	procThreadAttributeJobList       = 0x0002000D // ProcThreadAttributeValue(13, FALSE, TRUE, FALSE).
	processWaitExitCode              = 1
	jobAccountingInfoClass     int32 = windows.JobObjectBasicAccountingInformation
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

type windowsContainedProcess struct {
	job       windows.Handle
	process   windows.Handle
	thread    windows.Handle
	stdout    *os.File
	stderr    *os.File
	stdoutBuf limitedBuffer
	stderrBuf limitedBuffer
	stdoutEnd chan error
	stderrEnd chan error
}

func ensureNativeWindowsHost() error {
	if isNativeWSLEnvironment() {
		return errors.New("bridge startup rejected because its environment declares WSL integration; DSH plugin behavior is not inspected")
	}
	return nil
}

func isNativeWSLEnvironment() bool {
	for _, key := range []string{"WSL_DISTRO_NAME", "WSL_INTEROP", "WSLENV"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func validateNativeWindowsExecutable(path string) error {
	if isWSLBackedConfig(path, "", "", nil) || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return errors.New("DeepSeek Harness entry point must be a native Windows .exe; direct WSL commands and scripts are unsupported, but plugin-invoked WSL is not detected")
	}
	return nil
}

func startSuspendedProcess(spec processSpec) (suspendedProcess, error) {
	if err := ensureNativeWindowsHost(); err != nil {
		return nil, err
	}
	if err := validateNativeWindowsExecutable(spec.Path); err != nil {
		return nil, err
	}
	if strings.ContainsRune(spec.Path, 0) || strings.ContainsRune(spec.Dir, 0) {
		return nil, errors.New("invalid native process path")
	}
	for _, arg := range spec.Args {
		if strings.ContainsRune(arg, 0) {
			return nil, errors.New("invalid native process argument")
		}
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create Windows process job: %w", err)
	}
	jobOwned := true
	var info windows.ProcessInformation
	var stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite windows.Handle
	var stdoutFile, stderrFile *os.File
	created := false
	cleanup := func() {
		if !created {
			if info.Process != 0 {
				_ = windows.TerminateJobObject(job, processWaitExitCode)
				_ = windows.CloseHandle(info.Process)
				info.Process = 0
			}
			if info.Thread != 0 {
				_ = windows.CloseHandle(info.Thread)
				info.Thread = 0
			}
			if stdoutFile != nil {
				_ = stdoutFile.Close()
			}
			if stderrFile != nil {
				_ = stderrFile.Close()
			}
			for _, handle := range []*windows.Handle{&stdinRead, &stdinWrite, &stdoutRead, &stdoutWrite, &stderrRead, &stderrWrite} {
				closeWindowsHandle(handle)
			}
			if jobOwned {
				_ = windows.CloseHandle(job)
			}
		}
	}
	defer cleanup()

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, limitErr := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
	runtime.KeepAlive(&limits)
	if limitErr != nil {
		return nil, fmt.Errorf("configure kill-on-close Windows process job: %w", limitErr)
	}

	if err := createInheritedPipe(&stdinRead, &stdinWrite, true); err != nil {
		return nil, fmt.Errorf("create contained process input pipe: %w", err)
	}
	if err := createInheritedPipe(&stdoutRead, &stdoutWrite, false); err != nil {
		return nil, fmt.Errorf("create contained process output pipe: %w", err)
	}
	if err := createInheritedPipe(&stderrRead, &stderrWrite, false); err != nil {
		return nil, fmt.Errorf("create contained process error pipe: %w", err)
	}

	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, fmt.Errorf("prepare restricted process attributes: %w", err)
	}
	defer attributes.Delete()
	inheritedHandles := []windows.Handle{stdinRead, stdoutWrite, stderrWrite}
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&inheritedHandles[0]), uintptr(len(inheritedHandles))*unsafe.Sizeof(inheritedHandles[0])); err != nil {
		return nil, fmt.Errorf("restrict inherited process handles: %w", err)
	}
	jobHandles := []windows.Handle{job}
	if err := attributes.Update(procThreadAttributeJobList, unsafe.Pointer(&jobHandles[0]), uintptr(len(jobHandles))*unsafe.Sizeof(jobHandles[0])); err != nil {
		return nil, fmt.Errorf("require job assignment during process creation: %w", err)
	}

	startup := windows.StartupInfoEx{}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.StdInput, startup.StdOutput, startup.StdErr = stdinRead, stdoutWrite, stderrWrite
	startup.ProcThreadAttributeList = attributes.List()
	appName, err := windows.UTF16PtrFromString(spec.Path)
	if err != nil {
		return nil, fmt.Errorf("encode native executable path: %w", err)
	}
	commandArgs := append([]string{spec.Path}, spec.Args...)
	commandLine, err := windows.UTF16FromString(windows.ComposeCommandLine(commandArgs))
	if err != nil {
		return nil, fmt.Errorf("encode native process arguments: %w", err)
	}
	var currentDirectory *uint16
	if spec.Dir != "" {
		currentDirectory, err = windows.UTF16PtrFromString(spec.Dir)
		if err != nil {
			return nil, fmt.Errorf("encode native working directory: %w", err)
		}
	}
	environment, err := windowsEnvironmentBlock(spec.Env)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_NO_WINDOW)
	err = windows.CreateProcess(appName, &commandLine[0], nil, nil, true, flags, &environment[0], currentDirectory, (*windows.StartupInfo)(unsafe.Pointer(&startup)), &info)
	if err != nil {
		return nil, fmt.Errorf("create suspended process with atomic job assignment: %w", err)
	}

	// PROC_THREAD_ATTRIBUTE_JOB_LIST (Windows 10 / Server 2016 or newer) places
	// the still-suspended process in its kill-on-close job as part of
	// CreateProcess. There is no unsuspended window and no fallback to post-start
	// assignment if the host lacks this API; CreateProcess must fail closed.
	closeWindowsHandle(&stdinRead)
	closeWindowsHandle(&stdinWrite) // EOF for the child's stdin.
	closeWindowsHandle(&stdoutWrite)
	closeWindowsHandle(&stderrWrite)
	stdoutFile = os.NewFile(uintptr(stdoutRead), "hai-dsh-stdout")
	if stdoutFile == nil {
		return nil, errors.New("wrap contained process output pipe")
	}
	stdoutRead = 0
	stderrFile = os.NewFile(uintptr(stderrRead), "hai-dsh-stderr")
	if stderrFile == nil {
		return nil, errors.New("wrap contained process error pipe")
	}
	stderrRead = 0

	contained := &windowsContainedProcess{
		job:       job,
		process:   info.Process,
		thread:    info.Thread,
		stdout:    stdoutFile,
		stderr:    stderrFile,
		stdoutEnd: make(chan error, 1),
		stderrEnd: make(chan error, 1),
	}
	contained.stdoutBuf.remaining = maxOutputBytes
	contained.stderrBuf.remaining = maxOutputBytes / 4
	go copyProcessOutputAsync(stdoutFile, &contained.stdoutBuf, contained.stdoutEnd)
	go copyProcessOutputAsync(stderrFile, &contained.stderrBuf, contained.stderrEnd)
	created = true
	jobOwned = false
	return contained, nil
}

func createInheritedPipe(read, write *windows.Handle, inheritRead bool) error {
	if err := windows.CreatePipe(read, write, nil, 0); err != nil {
		return err
	}
	readFlags := uint32(0)
	writeFlags := uint32(windows.HANDLE_FLAG_INHERIT)
	if inheritRead {
		readFlags = windows.HANDLE_FLAG_INHERIT
		writeFlags = 0
	}
	if err := windows.SetHandleInformation(*read, windows.HANDLE_FLAG_INHERIT, readFlags); err != nil {
		closeWindowsHandle(read)
		closeWindowsHandle(write)
		return err
	}
	if err := windows.SetHandleInformation(*write, windows.HANDLE_FLAG_INHERIT, writeFlags); err != nil {
		closeWindowsHandle(read)
		closeWindowsHandle(write)
		return err
	}
	return nil
}

func windowsEnvironmentBlock(environment []string) ([]uint16, error) {
	environment = append([]string(nil), environment...)
	sort.Slice(environment, func(i, j int) bool {
		keyI, _, _ := strings.Cut(environment[i], "=")
		keyJ, _, _ := strings.Cut(environment[j], "=")
		return strings.ToUpper(keyI) < strings.ToUpper(keyJ)
	})
	block := make([]uint16, 0, 2)
	seen := make(map[string]struct{}, len(environment))
	for _, entry := range environment {
		if strings.ContainsRune(entry, 0) || !strings.Contains(entry, "=") {
			return nil, errors.New("invalid explicit process environment")
		}
		key, _, _ := strings.Cut(entry, "=")
		normalizedKey := strings.ToUpper(key)
		if _, exists := seen[normalizedKey]; exists {
			return nil, errors.New("duplicate explicit process environment key")
		}
		seen[normalizedKey] = struct{}{}
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, fmt.Errorf("encode explicit process environment: %w", err)
		}
		block = append(block, encoded[:len(encoded)-1]...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	block = append(block, 0)
	return block, nil
}

func copyProcessOutputAsync(file *os.File, destination *limitedBuffer, finished chan<- error) {
	err := copyProcessOutput(file, destination)
	finished <- err
	close(finished)
}

func (p *windowsContainedProcess) Resume() error {
	if p.thread == 0 {
		return errors.New("contained process primary thread is unavailable")
	}
	previousSuspendCount, err := windows.ResumeThread(p.thread)
	if err != nil {
		return fmt.Errorf("resume suspended contained process: %w", err)
	}
	if previousSuspendCount != 1 {
		return fmt.Errorf("contained process primary thread had unexpected suspend count %d", previousSuspendCount)
	}
	return nil
}

func (p *windowsContainedProcess) Wait(ctx context.Context) (processResult, error) {
	if err := p.waitForJobEmpty(ctx); err != nil {
		return processResult{ExitCode: -1}, err
	}
	state, err := windows.WaitForSingleObject(p.process, uint32(processWaitDelay/time.Millisecond))
	if err != nil {
		return processResult{ExitCode: -1}, fmt.Errorf("confirm contained root process exit: %w", err)
	}
	if state != windows.WAIT_OBJECT_0 {
		return processResult{ExitCode: -1}, errors.New("contained job emptied but the root process handle was not signaled")
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(p.process, &exitCode); err != nil {
		return processResult{ExitCode: -1}, fmt.Errorf("read contained process exit code: %w", err)
	}
	if err := p.waitForOutput(ctx); err != nil {
		return processResult{ExitCode: int(exitCode), Stdout: p.stdoutBuf.String(), Stderr: p.stderrBuf.String()}, err
	}
	return processResult{ExitCode: int(exitCode), Stdout: p.stdoutBuf.String(), Stderr: p.stderrBuf.String()}, nil
}

func (p *windowsContainedProcess) waitForJobEmpty(ctx context.Context) error {
	ticker := time.NewTicker(processWaitInterval)
	defer ticker.Stop()
	for {
		var accounting jobBasicAccountingInformation
		queryErr := windows.QueryInformationJobObject(p.job, jobAccountingInfoClass, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
		runtime.KeepAlive(&accounting)
		if queryErr != nil {
			return fmt.Errorf("inspect contained process job: %w", queryErr)
		}
		if accounting.ActiveProcesses == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *windowsContainedProcess) waitForOutput(ctx context.Context) error {
	// This raw CreateProcess supervisor cannot use exec.Cmd.WaitDelay. A bounded
	// post-job pipe-drain deadline provides the same additional guard; the job
	// is still terminated first on cancellation and is never replaced by it.
	timer := time.NewTimer(processWaitDelay)
	defer timer.Stop()
	stdoutEnd, stderrEnd := (<-chan error)(p.stdoutEnd), (<-chan error)(p.stderrEnd)
	var captureErr error
	for stdoutEnd != nil || stderrEnd != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case copyErr := <-stdoutEnd:
			stdoutEnd = nil
			captureErr = errors.Join(captureErr, outputCaptureError("stdout", copyErr))
		case copyErr := <-stderrEnd:
			stderrEnd = nil
			captureErr = errors.Join(captureErr, outputCaptureError("stderr", copyErr))
		case <-timer.C:
			p.closeOutputPipes()
			return errors.Join(captureErr, fmt.Errorf("%w: output handles did not close within the bounded wait delay", errOutputCaptureIncomplete))
		}
	}
	return captureErr
}

func (p *windowsContainedProcess) Terminate() error {
	if p.job == 0 {
		return fmt.Errorf("%w: Windows Job Object handle is unavailable", errProcessTreeTerminationUnverified)
	}
	_ = windows.TerminateJobObject(p.job, processWaitExitCode)
	waitContext, cancel := context.WithTimeout(context.Background(), processTerminationWait)
	defer cancel()
	if err := p.waitForJobEmpty(waitContext); err == nil {
		return nil
	} else {
		closeErr := p.closeJob()
		var rootExitErr error
		if p.process != 0 {
			state, waitErr := windows.WaitForSingleObject(p.process, uint32(processTerminationWait/time.Millisecond))
			if waitErr != nil {
				rootExitErr = fmt.Errorf("verify root exit after kill-on-close: %w", waitErr)
			} else if state != windows.WAIT_OBJECT_0 {
				rootExitErr = errors.New("root process remained unsignaled after kill-on-close")
			}
		}
		if closeErr != nil {
			return errors.Join(fmt.Errorf("contained process job did not empty before termination deadline: %w", err), fmt.Errorf("close kill-on-close job: %w", closeErr), rootExitErr)
		}
		return errors.Join(fmt.Errorf("contained process job did not empty before termination deadline; kill-on-close fallback applied, but descendant termination completion could not be verified: %w", err), rootExitErr)
	}
}

func (p *windowsContainedProcess) Close() error {
	var closeErrors []error
	if err := p.closeJob(); err != nil {
		closeErrors = append(closeErrors, err)
	}
	for _, handle := range []*windows.Handle{&p.thread, &p.process} {
		if err := closeWindowsHandle(handle); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	if p.stdout != nil {
		if err := p.stdout.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
		p.stdout = nil
	}
	if p.stderr != nil {
		if err := p.stderr.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
		p.stderr = nil
	}
	return errors.Join(closeErrors...)
}

func (p *windowsContainedProcess) closeJob() error {
	return closeWindowsHandle(&p.job)
}

func (p *windowsContainedProcess) closeOutputPipes() {
	if p.stdout != nil {
		_ = p.stdout.Close()
		p.stdout = nil
	}
	if p.stderr != nil {
		_ = p.stderr.Close()
		p.stderr = nil
	}
}

func closeWindowsHandle(handle *windows.Handle) error {
	if handle == nil || *handle == 0 {
		return nil
	}
	err := windows.CloseHandle(*handle)
	if err == nil {
		*handle = 0
	}
	return err
}

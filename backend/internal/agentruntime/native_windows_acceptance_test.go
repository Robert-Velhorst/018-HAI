//go:build windows

package agentruntime

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"automation-hub-backend/internal/pathsafety"
	"automation-hub-backend/internal/safety"

	"golang.org/x/sys/windows"
)

const windowsAcceptanceGuard = "synthetic-only-v1"

// This is opt-in native regression proof, not sandbox or installed-product
// acceptance. No t.TempDir is used: all synthetic evidence is retained.
func windowsAcceptanceRoot() (string, error) {
	if os.Getenv("HAI_WINDOWS_NATIVE_ACCEPTANCE") != windowsAcceptanceGuard {
		return "", errors.New("explicit synthetic-only acceptance opt-in is required")
	}
	root := os.Getenv("HAI_WINDOWS_NATIVE_ACCEPTANCE_ROOT")
	if !filepath.IsAbs(root) || !strings.HasPrefix(filepath.Base(root), "hai-windows-native-acceptance-") {
		return "", errors.New("runner-created absolute scratch root is required")
	}
	root, err := pathsafety.ValidateNoLinks(root, false)
	if err != nil {
		return "", err
	}
	marker, err := os.ReadFile(filepath.Join(root, "synthetic-only.marker"))
	if err != nil || string(marker) != windowsAcceptanceGuard {
		return "", errors.New("synthetic-only marker is missing or invalid")
	}
	executable, err := os.Executable()
	if err != nil || !strings.EqualFold(filepath.Dir(executable), root) {
		return "", errors.New("test executable must be built in the guarded scratch root")
	}
	return root, nil
}

type windowsAcceptanceSnapshot struct {
	Args      []string
	Directory string
	State     string
	Home      string
	Profile   string
	Temp      string
	TaskID    string
	Sentinel  string
}

// The only executable fixture is this test binary. It never starts descendants,
// evaluates a prompt, opens a socket, or invokes a shell/provider/runtime.
func TestWindowsNativeAcceptanceHelper(t *testing.T) {
	mode := os.Getenv("HAI_NATIVE_FIXTURE_MODE")
	if mode == "" {
		return
	}
	root, err := windowsAcceptanceRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		t.Fatal("helper may only execute inside its synthetic workspace")
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || len(os.Args[separator+1:]) != 3 || os.Args[separator+1] != "--profile" || os.Args[separator+2] != "headless" {
		t.Fatal("helper received an unexpected headless contract")
	}
	switch mode {
	case "snapshot":
		if err := json.NewEncoder(os.Stdout).Encode(windowsAcceptanceSnapshot{
			Args: os.Args[separator+1:], Directory: dir,
			State: os.Getenv("DSH_HOME"), Home: os.Getenv("HOME"),
			Profile: os.Getenv("USERPROFILE"), Temp: os.Getenv("TEMP"),
			TaskID: os.Getenv("HAI_RUNTIME_TASK_ID"), Sentinel: os.Getenv("HAI_NATIVE_CREDENTIAL_SENTINEL"),
		}); err != nil {
			t.Fatal(err)
		}
	case "flood":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 16384))
	case "exit":
		fmt.Fprint(os.Stderr, "synthetic exit diagnostic")
		os.Exit(23)
	case "wait":
		file, err := os.OpenFile(filepath.Join(dir, "helper-ready.marker"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprint(file, "synthetic helper ready"); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(8 * time.Second) // Bounds orphan lifetime even if the parent aborts.
		os.Exit(91)
	default:
		t.Fatal("unknown synthetic helper mode")
	}
	os.Exit(0)
}

type windowsAcceptanceFixture struct {
	root      string
	workspace string
	state     string
	outside   string
	adapter   *deepSeekHarnessAdapter
}

func newWindowsAcceptanceFixture(t *testing.T, root, mode string) windowsAcceptanceFixture {
	t.Helper()
	base, err := os.MkdirTemp(root, "fixture-")
	if err != nil {
		t.Fatal(err)
	}
	f := windowsAcceptanceFixture{root: filepath.Join(base, "allowed"), outside: filepath.Join(base, "outside")}
	f.workspace = filepath.Join(f.root, "workspace")
	f.state = filepath.Join(f.root, "state")
	for _, dir := range []string{f.root, f.outside, f.workspace, f.state} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HAI_NATIVE_FIXTURE_MODE", mode)
	t.Setenv("HAI_DSH_TEST_VERSION_OUTPUT", "0.1.7-alpha.2")
	f.adapter = &deepSeekHarnessAdapter{
		enabled: true, executionEnabled: true, executable: executable,
		expectedVersion: "0.1.7-alpha.2", workspace: f.workspace,
		workspaceRoot: f.root, stateDir: f.state, timeout: 3 * time.Second,
		outputLimit: 4096,
		envAllow:    []string{"HAI_WINDOWS_NATIVE_ACCEPTANCE", "HAI_WINDOWS_NATIVE_ACCEPTANCE_ROOT", "HAI_NATIVE_FIXTURE_MODE", "HAI_DSH_TEST_VERSION_OUTPUT"},
		// Existing private test seam only; production construction stays blocked.
		allowDirectExecutionForTest: true,
		processStarter: func(cmd *exec.Cmd) error {
			if cmd.Path != executable || len(cmd.Args) != 4 || cmd.Args[1] != "--profile" || cmd.Args[2] != "headless" {
				return errors.New("unexpected runtime command; refusing to start")
			}
			cmd.Args = append([]string{executable, "-test.run=^TestWindowsNativeAcceptanceHelper$", "--"}, cmd.Args[1:]...)
			return cmd.Start()
		},
	}
	t.Logf("retained synthetic fixture: %s", base)
	return f
}

func windowsAcceptanceTask() Task {
	return Task{ID: "synthetic-native-task", Prompt: "synthetic read-only fixture", OwnerIdentity: "synthetic-owner"}
}

func windowsAcceptanceNoStart(t *testing.T, adapter *deepSeekHarnessAdapter) *int {
	t.Helper()
	starts := new(int)
	adapter.processStarter = func(*exec.Cmd) error {
		(*starts)++
		return errors.New("unexpected process start denied by acceptance fixture")
	}
	return starts
}

func TestWindowsNativeAcceptance(t *testing.T) {
	if os.Getenv("HAI_WINDOWS_NATIVE_ACCEPTANCE") == "" {
		t.Skip("use scripts/test-windows-native-runtime.ps1 for guarded native acceptance")
	}
	root, err := windowsAcceptanceRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"HOME", "USERPROFILE", "TEMP", "TMP"} {
		if !strings.EqualFold(os.Getenv(name), root) {
			t.Fatalf("%s must refer only to the synthetic scratch root", name)
		}
	}
	t.Log("native Windows synthetic regression; no sandbox, descendant-tree, provider, or installed-product acceptance is claimed")

	t.Run("guard_fails_closed", func(t *testing.T) {
		t.Setenv("HAI_WINDOWS_NATIVE_ACCEPTANCE", "false")
		if _, err := windowsAcceptanceRoot(); err == nil {
			t.Fatal("missing synthetic-only guard was accepted")
		}
	})
	t.Run("disabled_defaults", func(t *testing.T) {
		for _, name := range []string{"DEEPSEEK_HARNESS_ENABLED", "DEEPSEEK_HARNESS_EXECUTION_ENABLED", "HERMES_AGENT_ENABLED", "OPENCLAW_AGENT_ENABLED", "OPENCLAW_GATEWAY_DELEGATION_ENABLED"} {
			t.Setenv(name, "")
		}
		for _, adapter := range []Adapter{newDeepSeekHarnessAdapterFromEnv(), newHermesAdapterFromEnv(), newOpenClawAdapterFromEnv()} {
			result := adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
			if adapter.Info().Enabled || result.Status != "blocked" || result.ExitCode != -1 {
				t.Fatalf("default %s did not deny execution: %#v", adapter.Info().ID, result)
			}
		}
	})
	t.Run("production_isolation_not_bypassed", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		f.adapter.allowDirectExecutionForTest = false
		starts := windowsAcceptanceNoStart(t, f.adapter)
		result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
		if *starts != 0 || f.adapter.Info().ExecutionEnabled || result.Status != "blocked" || !strings.Contains(result.Message, "OS-enforced least-privilege sandbox") {
			t.Fatalf("production DSH isolation was bypassed: starts=%d result=%#v", *starts, result)
		}
		for _, adapter := range []Adapter{&hermesAdapter{enabled: true, executable: f.adapter.executable}, &openClawAdapter{enabled: true, agentCLIEnabled: true, executable: f.adapter.executable}} {
			if result := adapter.ExecuteTask(context.Background(), windowsAcceptanceTask()); result.Status != "blocked" {
				t.Fatalf("CLI executed without tool mediation: %#v", result)
			}
		}
	})
	t.Run("workspace_and_state_escape", func(t *testing.T) {
		for _, target := range []string{"workspace", "state", "missing-root", "state-file", "missing-workspace"} {
			t.Run(target, func(t *testing.T) {
				f := newWindowsAcceptanceFixture(t, root, "snapshot")
				switch target {
				case "workspace":
					f.adapter.workspace = f.outside
				case "state":
					f.adapter.stateDir = f.outside
				case "missing-root":
					f.adapter.workspaceRoot = ""
				case "state-file":
					f.adapter.stateDir = filepath.Join(f.root, "not-a-directory")
					if err := os.WriteFile(f.adapter.stateDir, []byte("synthetic"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "missing-workspace":
					f.adapter.workspace = filepath.Join(f.root, "absent")
				}
				starts := windowsAcceptanceNoStart(t, f.adapter)
				result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
				if result.Status != "blocked" || *starts != 0 {
					t.Fatalf("path restriction failed: starts=%d result=%#v", *starts, result)
				}
			})
		}
	})
	t.Run("windows_junction_containment", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		const outsideEvidence = "synthetic evidence only present outside the allowed root"
		if err := os.WriteFile(filepath.Join(f.outside, "outside-only.txt"), []byte(outsideEvidence), 0o600); err != nil {
			t.Fatal(err)
		}
		junction := filepath.Join(f.root, "escape-junction")
		if err := windowsAcceptanceJunction(junction, f.outside); err != nil {
			t.Fatalf("native synthetic junction fixture unavailable (not a pass): %v", err)
		}
		if content, err := os.ReadFile(filepath.Join(junction, "outside-only.txt")); err != nil || string(content) != outsideEvidence {
			t.Fatalf("junction did not actually redirect outside the synthetic allowed root: %v", err)
		}
		if err := os.Mkdir(filepath.Join(f.outside, "nested-workspace"), 0o700); err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(junction)
		t.Logf("native junction redirects to %s; Go EvalSymlinks returned %s (err=%v)", f.outside, resolved, err)
		if secured, err := pathsafety.OpenSecureRoot(junction, false); !errors.Is(err, pathsafety.ErrPathLink) {
			if secured != nil {
				_ = secured.Close()
			}
			t.Fatalf("secure workspace did not reject a native junction: %v", err)
		}
		starts := windowsAcceptanceNoStart(t, f.adapter)
		probes := 0
		f.adapter.versionProbe = func(context.Context) (string, error) {
			probes++
			return "0.1.7-alpha.2", nil
		}
		for _, target := range []string{"workspace", "workspace-ancestor", "workspace-root", "state"} {
			t.Run(target, func(t *testing.T) {
				f.adapter.workspaceRoot, f.adapter.workspace, f.adapter.stateDir = f.root, f.workspace, f.state
				switch target {
				case "workspace":
					f.adapter.workspace = junction
				case "workspace-ancestor":
					f.adapter.workspace = filepath.Join(junction, "nested-workspace")
				case "workspace-root":
					f.adapter.workspaceRoot = junction
					f.adapter.workspace = filepath.Join(junction, "nested-workspace")
				case "state":
					f.adapter.stateDir = junction
				}
				*starts, probes = 0, 0
				result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
				if result.Status != "blocked" || *starts != 0 || probes != 0 {
					t.Fatalf("junction escaped %s containment: probes=%d attempted_starts=%d result=%#v", target, probes, *starts, result)
				}
				t.Logf("junction %s denied before probe/start: %s", target, result.Message)
			})
		}
	})
	t.Run("rooted_file_contract", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		secured, err := pathsafety.OpenSecureRoot(f.workspace, false)
		if err != nil {
			t.Fatal(err)
		}
		defer secured.Close()
		file, _, err := secured.CreateExclusiveFile("synthetic.txt", 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("synthetic evidence"); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := secured.CreateExclusiveFile("synthetic.txt", 0o600); !errors.Is(err, pathsafety.ErrPathExists) {
			t.Fatalf("exclusive creation allowed overwrite: %v", err)
		}
		for _, name := range []string{`..\outside.txt`, filepath.Join(f.outside, "outside.txt"), "synthetic.txt:alternate-stream"} {
			if file, _, err := secured.CreateExclusiveFile(name, 0o600); err == nil {
				_ = file.Close()
				t.Fatalf("rooted file creation accepted unsafe Windows path %q", name)
			}
		}
	})
	t.Run("headless_argv_environment_and_review", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		t.Setenv("HAI_NATIVE_CREDENTIAL_SENTINEL", "synthetic-value-not-a-real-credential")
		task := windowsAcceptanceTask()
		task.Prompt = `synthetic literal & | ; $(not-a-command) "quoted"`
		result := f.adapter.ExecuteTask(context.Background(), task)
		if result.Status != "needs_review" || result.ExitCode != 0 {
			t.Fatalf("native helper did not return unverified success: %#v", result)
		}
		var snapshot windowsAcceptanceSnapshot
		if err := json.Unmarshal([]byte(result.Output), &snapshot); err != nil {
			t.Fatalf("native helper output: %v (%q)", err, result.Output)
		}
		if !reflect.DeepEqual(snapshot.Args, []string{"--profile", "headless", task.Prompt}) || !strings.EqualFold(snapshot.Directory, f.workspace) || !strings.EqualFold(snapshot.State, f.state) || snapshot.TaskID != task.ID || snapshot.Sentinel != "" {
			t.Fatalf("native headless contract mismatch: %#v", snapshot)
		}
		for _, path := range []string{snapshot.Home, snapshot.Profile, snapshot.Temp} {
			if !strings.EqualFold(path, root) {
				t.Fatalf("helper inherited a real host profile/temp path: %q", path)
			}
		}
	})
	t.Run("native_version_probe_mismatch", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		t.Setenv("HAI_DSH_TEST_VERSION_OUTPUT", "0.1.8")
		starts := windowsAcceptanceNoStart(t, f.adapter)
		result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
		if result.Status != "blocked" || !strings.Contains(result.Message, "version mismatch") || *starts != 0 {
			t.Fatalf("version mismatch allowed task process start: %#v", result)
		}
	})
	t.Run("bounded_output_and_nonzero_exit", func(t *testing.T) {
		for _, mode := range []string{"flood", "exit"} {
			t.Run(mode, func(t *testing.T) {
				f := newWindowsAcceptanceFixture(t, root, mode)
				result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
				if mode == "flood" && (result.Status != "needs_review" || len(result.Output) != int(f.adapter.outputLimit)) {
					t.Fatalf("output limit contract failed: %#v", result)
				}
				if mode == "exit" && (result.Status != "failed" || result.ExitCode != 23 || !strings.Contains(result.Message, "synthetic exit diagnostic")) {
					t.Fatalf("native exit status contract failed: %#v", result)
				}
			})
		}
	})
	t.Run("timeout_stops_actual_child", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "wait")
		// Probe separately so this timeout measures a running child, not startup.
		f.adapter.versionProbe = func(context.Context) (string, error) { return "0.1.7-alpha.2", nil }
		f.adapter.timeout = 700 * time.Millisecond
		var child *os.Process
		start := f.adapter.processStarter
		f.adapter.processStarter = func(cmd *exec.Cmd) error {
			err := start(cmd)
			child = cmd.Process
			return err
		}
		started := time.Now()
		result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
		if result.Status != "blocked" || !strings.Contains(result.Message, "timeout") || time.Since(started) > 4*time.Second {
			t.Fatalf("timeout did not return a bounded blocked result: %#v", result)
		}
		if _, err := os.Stat(filepath.Join(f.workspace, "helper-ready.marker")); err != nil {
			t.Fatalf("no proof the native child actually ran: %v", err)
		}
		windowsAcceptanceExited(t, child)
	})
	t.Run("cancellation_stops_actual_child", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "wait")
		f.adapter.timeout = 6 * time.Second
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started := make(chan *os.Process, 1)
		start := f.adapter.processStarter
		f.adapter.processStarter = func(cmd *exec.Cmd) error {
			err := start(cmd)
			if err == nil {
				started <- cmd.Process
			}
			return err
		}
		done := make(chan Result, 1)
		go func() { done <- f.adapter.ExecuteTask(ctx, windowsAcceptanceTask()) }()
		var child *os.Process
		select {
		case child = <-started:
		case <-time.After(4 * time.Second):
			cancel()
			<-done
			t.Fatal("native helper did not start")
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(f.workspace, "helper-ready.marker")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				cancel()
				<-done
				t.Fatal("native helper did not produce readiness evidence")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		select {
		case result := <-done:
			// Current direct-test seam reports failed, not durable cancellation.
			if result.Status != "failed" || result.ExitCode == 0 {
				t.Fatalf("cancelled native helper claimed successful completion: %#v", result)
			}
			t.Log("direct helper cancellation returns failed; no durable cancellation or descendant-tree proof claimed")
		case <-time.After(4 * time.Second):
			t.Fatal("native cancellation did not return within four seconds")
		}
		windowsAcceptanceExited(t, child)
	})
	t.Run("pre_cancel_and_busy_state_gate", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		starts := windowsAcceptanceNoStart(t, f.adapter)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if result := f.adapter.ExecuteTask(ctx, windowsAcceptanceTask()); result.Status != "blocked" || *starts != 0 {
			t.Fatalf("pre-cancelled task started: %#v", result)
		}
		if !f.adapter.acquireExecutionGate(context.Background()) {
			t.Fatal("cannot acquire synthetic state gate")
		}
		f.adapter.timeout = 100 * time.Millisecond
		result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask())
		f.adapter.releaseExecutionGate()
		if result.Status != "blocked" || !strings.Contains(result.Message, "already running") || *starts != 0 {
			t.Fatalf("busy shared-state gate allowed execution: %#v", result)
		}
	})
	t.Run("prompt_options_denied_before_start", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		starts := windowsAcceptanceNoStart(t, f.adapter)
		for _, prompt := range []string{"--version", "  --profile web", "web", "plugin", "synthetic\x00prompt"} {
			task := windowsAcceptanceTask()
			task.Prompt = prompt
			if result := f.adapter.ExecuteTask(context.Background(), task); result.Status != "blocked" || *starts != 0 {
				t.Fatalf("ambiguous prompt was started: %#v", result)
			}
		}
	})
	t.Run("emergency_stop_denies_helper", func(t *testing.T) {
		f := newWindowsAcceptanceFixture(t, root, "snapshot")
		starts := windowsAcceptanceNoStart(t, f.adapter)
		restore := safety.SetEmergencyStopProvider(safety.EmergencyStopProviderFunc(func() (bool, string, error) {
			return true, "synthetic acceptance stop", nil
		}))
		defer restore()
		if result := f.adapter.ExecuteTask(context.Background(), windowsAcceptanceTask()); result.Status != "blocked" || *starts != 0 {
			t.Fatalf("emergency stop allowed native child: %#v", result)
		}
	})
}

func windowsAcceptanceExited(t *testing.T, child *os.Process) {
	t.Helper()
	if child == nil {
		t.Fatal("no actual native child process was observed")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(child.Pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return // Windows has already retired this PID.
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 1000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("native child survived adapter return: wait=%d err=%v", state, err)
	}
}

// Build a mount-point reparse record directly; never invoke cmd/mklink or ask
// for symlink privileges. Both directories are new, guarded synthetic paths.
func windowsAcceptanceJunction(link, target string) error {
	if err := os.Mkdir(link, 0o700); err != nil {
		return err
	}
	substitute := append(utf16.Encode([]rune(`\??\`+target)), 0)
	printName := append(utf16.Encode([]rune(target)), 0)
	data := make([]byte, 16+2*(len(substitute)+len(printName)))
	binary.LittleEndian.PutUint32(data[0:4], 0xa0000003) // IO_REPARSE_TAG_MOUNT_POINT
	binary.LittleEndian.PutUint16(data[4:6], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:12], uint16(2*(len(substitute)-1)))
	binary.LittleEndian.PutUint16(data[12:14], uint16(2*len(substitute)))
	binary.LittleEndian.PutUint16(data[14:16], uint16(2*(len(printName)-1)))
	for i, value := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(data[16+2*i:], value)
	}
	name, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var returned uint32
	return windows.DeviceIoControl(handle, 0x000900a4, &data[0], uint32(len(data)), nil, 0, &returned, nil) // FSCTL_SET_REPARSE_POINT
}

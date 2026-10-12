package openclawmaintenance

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func writeMaintenanceOverflow(writer io.Writer) {
	chunk := []byte(strings.Repeat("x", 16<<10))
	remaining := maintenanceCommandOutputLimit + 1
	for remaining > 0 {
		writeSize := len(chunk)
		if writeSize > remaining {
			writeSize = remaining
		}
		written, err := writer.Write(chunk[:writeSize])
		if err != nil || written <= 0 {
			return
		}
		remaining -= written
	}
}

func TestMaintenanceCommandHelperProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg == "--maintenance-command-helper-failure" {
			_, _ = fmt.Fprint(os.Stdout, "stdout-secret-sentinel")
			_, _ = fmt.Fprint(os.Stderr, "stderr-secret-sentinel")
			os.Exit(1)
		}
		if arg == "--maintenance-command-helper" {
			_, _ = fmt.Fprint(os.Stdout, "maintenance-command-helper-ran")
			return
		}
		if arg == "--maintenance-command-helper-overflow" {
			writeMaintenanceOverflow(os.Stdout)
			time.Sleep(30 * time.Second)
			return
		}
		if arg == "--maintenance-authenticode-helper-overflow-stdout" {
			writeMaintenanceOverflow(os.Stdout)
			time.Sleep(30 * time.Second)
			return
		}
		if arg == "--maintenance-authenticode-helper-overflow-stderr" {
			writeMaintenanceOverflow(os.Stderr)
			time.Sleep(30 * time.Second)
			return
		}
	}
}

func TestTrustedSystemCommandPathUsesOnlySystemDirectoryTargets(t *testing.T) {
	systemDirectory := t.TempDir()
	attackerPathDirectory := t.TempDir()
	t.Setenv("PATH", attackerPathDirectory)

	for _, path := range []string{
		filepath.Join(systemDirectory, "wsl.exe"),
		filepath.Join(systemDirectory, "WindowsPowerShell", "v1.0", "powershell.exe"),
		filepath.Join(attackerPathDirectory, "wsl.exe"),
		filepath.Join(attackerPathDirectory, "powershell.exe"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("untrusted test fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	for _, name := range []string{"wsl.exe", "powershell.exe"} {
		got, err := trustedSystemCommandPath(systemDirectory, name)
		want := filepath.Join(systemDirectory, name)
		if name == "powershell.exe" {
			want = filepath.Join(systemDirectory, "WindowsPowerShell", "v1.0", name)
		}
		if err != nil || got != want || strings.Contains(strings.ToLower(got), strings.ToLower(attackerPathDirectory)) {
			t.Fatalf("trusted system resolution for %q = %q, %v; want %q outside PATH attacker directory", name, got, err, want)
		}
	}
	for _, name := range []string{"wsl", "cmd.exe", filepath.Join(attackerPathDirectory, "wsl.exe")} {
		if _, err := trustedSystemCommandPath(systemDirectory, name); err == nil {
			t.Errorf("accepted non-allowlisted command %q", name)
		}
	}
	if _, err := trustedSystemCommandPath("relative-system-directory", "wsl.exe"); err == nil {
		t.Fatal("accepted a relative system directory")
	}
}

func TestTrustedMaintenanceCommandPathNeverFallsBackToPATH(t *testing.T) {
	attackerPathDirectory := t.TempDir()
	for _, name := range []string{"wsl.exe", "powershell.exe"} {
		if err := os.WriteFile(filepath.Join(attackerPathDirectory, name), []byte("attacker"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", attackerPathDirectory)

	for _, name := range []string{"wsl.exe", "powershell.exe"} {
		got, err := trustedMaintenanceCommandPath(name)
		if err == nil && strings.EqualFold(filepath.Dir(got), attackerPathDirectory) {
			t.Fatalf("resolved %q from attacker-controlled PATH: %q", name, got)
		}
		if runtime.GOOS != "windows" && err == nil {
			t.Fatalf("platform without trusted Windows system paths resolved %q: %q", name, got)
		}
	}
}

func TestTrustedSystemCommandPathFailsClosedWhenExecutableIsMissingOrLinked(t *testing.T) {
	systemDirectory := t.TempDir()
	if _, err := trustedSystemCommandPath(systemDirectory, "wsl.exe"); err == nil {
		t.Fatal("resolved a missing system executable")
	}

	attackerExecutable := filepath.Join(t.TempDir(), "wsl.exe")
	if err := os.WriteFile(attackerExecutable, []byte("attacker"), 0600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(systemDirectory, "wsl.exe")
	if err := os.Symlink(attackerExecutable, linkPath); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	if _, err := trustedSystemCommandPath(systemDirectory, "wsl.exe"); err == nil {
		t.Fatal("accepted a symlink in the trusted system directory")
	}
}

func TestCommandUsesConfiguredAbsolutePathWithoutPATHLookup(t *testing.T) {
	attackerPathDirectory := t.TempDir()
	attackerExecutable := filepath.Join(attackerPathDirectory, "wsl.exe")
	if err := os.WriteFile(attackerExecutable, []byte("attacker"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", attackerPathDirectory)
	configuredExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	resolved := 0
	output, err := commandWithResolver(context.Background(), func(name string) (string, error) {
		resolved++
		if name != "wsl.exe" {
			t.Fatalf("resolver was asked for %q", name)
		}
		return configuredExecutable, nil
	}, "wsl.exe", nil, "-test.run=^TestMaintenanceCommandHelperProcess$", "--", "--maintenance-command-helper")
	if err != nil || resolved != 1 || !strings.Contains(string(output), "maintenance-command-helper-ran") {
		t.Fatalf("explicit configured command path did not run: calls=%d output=%q err=%v", resolved, output, err)
	}

	if _, err := commandWithResolver(context.Background(), func(string) (string, error) { return "wsl.exe", nil }, "wsl.exe", nil); err == nil {
		t.Fatal("accepted a non-absolute command path that could trigger PATH lookup")
	}
	if _, err := commandWithResolver(context.Background(), func(string) (string, error) { return "", errors.New("system directory unavailable") }, "wsl.exe", nil); err == nil {
		t.Fatal("did not fail closed when trusted command resolution failed")
	}
}

func TestCommandFailureDoesNotReturnChildOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, err := commandWithResolver(context.Background(), func(string) (string, error) {
		return executable, nil
	}, "wsl.exe", nil, "-test.run=^TestMaintenanceCommandHelperProcess$", "--", "--maintenance-command-helper-failure")
	if err == nil || err.Error() != "maintenance command failed" {
		t.Fatalf("command failure was not normalized: output=%q err=%v", output, err)
	}
	if len(output) != 0 || strings.Contains(err.Error(), "stdout-secret-sentinel") || strings.Contains(err.Error(), "stderr-secret-sentinel") {
		t.Fatalf("child output escaped failure redaction: output=%q err=%v", output, err)
	}
}

func TestCommandOutputLimitStopsProcessWithoutBlockingPipe(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := commandWithResolver(ctx, func(string) (string, error) {
		return executable, nil
	}, "wsl.exe", nil, "-test.run=^TestMaintenanceCommandHelperProcess$", "--", "--maintenance-command-helper-overflow")
	if err == nil || err.Error() != "maintenance command output limit exceeded" {
		t.Fatalf("excessive command output was not stopped and reported: output=%d bytes err=%v", len(output), err)
	}
	if len(output) != 0 {
		t.Fatalf("excessive output escaped the command boundary: %d bytes", len(output))
	}
	if ctx.Err() != nil {
		t.Fatalf("command needed the caller deadline to stop after output overflow: %v", ctx.Err())
	}
}

func TestAuthenticodeVerificationOutputLimitStopsProcessWithoutBlockingPipe(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := verifyCompanionAuthenticodeWith(ctx, "installer.exe", strings.Repeat("A", 40), func(string) (string, error) {
				return executable, nil
			}, "-test.run=^TestMaintenanceCommandHelperProcess$", "--", "--maintenance-authenticode-helper-overflow-"+stream)
			if err == nil || err.Error() != "installer publisher verification output limit exceeded" {
				t.Fatalf("excessive %s output was not stopped and reported: err=%v", stream, err)
			}
			if ctx.Err() != nil {
				t.Fatalf("verification needed the caller deadline to stop after %s overflow: %v", stream, ctx.Err())
			}
		})
	}
}

func TestGatewayUpdateCommandPinsOfficialRegistryAndExactVersion(t *testing.T) {
	want := []string{
		"--distribution", "OpenClawGateway", "--exec", "env",
		"npm_config_registry=" + officialNPMRegistry,
		"NPM_CONFIG_REGISTRY=" + officialNPMRegistry,
		"pnpm_config_registry=" + officialNPMRegistry,
		"PNPM_CONFIG_REGISTRY=" + officialNPMRegistry,
		"BUN_CONFIG_REGISTRY=" + officialNPMRegistry,
		"openclaw", "update", "--tag", "2026.9.4", "--json", "--yes", "--timeout", "600",
	}
	args, err := coreUpdateCommandArgs("OpenClawGateway", "2026.9.4")
	if err != nil || strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("Gateway update was not pinned to the verified registry and exact release: args=%q err=%v", args, err)
	}
	for _, version := range []string{"latest", "2026.9", "2026.9.4-beta.1", "2026.9.4;whoami"} {
		if _, err := coreUpdateCommandArgs("OpenClawGateway", version); err == nil {
			t.Errorf("accepted non-exact Gateway release target %q", version)
		}
	}
}

func TestRegistrySignaturesRejectTamperedPackageAndIdentity(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(make([]byte, 64))
	msg := sha256.Sum256([]byte("openclaw@2026.9.1:" + integrity))
	sig, _ := ecdsa.SignASN1(rand.Reader, key, msg[:])
	tests := []struct {
		name, packageName, packageVersion, usedIntegrity, keyID string
		wantOK                                                  bool
	}{
		{"valid", "openclaw", "2026.9.1", integrity, "test-key", true},
		{"changed integrity", "openclaw", "2026.9.1", "sha512-" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 64))), "test-key", false},
		{"untrusted signer id", "openclaw", "2026.9.1", integrity, "registry-supplied-key", false},
		{"wrong package", "lookalike", "2026.9.1", integrity, "test-key", false},
		{"wrong version", "openclaw", "2026.9.2", integrity, "test-key", false},
		{"malformed integrity", "openclaw", "2026.9.1", "sha512-not-base64", "test-key", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorker()
			w.registryKeyID = "test-key"
			w.registryPublicKey = base64.StdEncoding.EncodeToString(pub)
			keysRequests := 0
			payload := map[string]any{"name": tt.packageName, "version": tt.packageVersion, "dist": map[string]any{
				"integrity":  tt.usedIntegrity,
				"signatures": []any{map[string]any{"keyid": tt.keyID, "sig": base64.StdEncoding.EncodeToString(sig)}},
			}}
			w.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/-/npm/v1/keys" {
					keysRequests++
					payload = map[string]any{"keys": []any{map[string]any{"keyid": "registry-supplied-key", "key": base64.StdEncoding.EncodeToString(pub)}}}
				}
				b, _ := json.Marshal(payload)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})
			evidence, err := w.coreRelease(context.Background(), "2026.9.1")
			if (err == nil) != tt.wantOK {
				t.Fatalf("signature result evidence=%q err=%v wantOK=%v", evidence, err, tt.wantOK)
			}
			if tt.wantOK && len(evidence) != 64 {
				t.Fatalf("valid signature produced invalid evidence: %q", evidence)
			}
			if keysRequests != 0 {
				t.Fatal("verification fetched its trust key from the package registry")
			}
		})
	}
}

func TestRegistryKeyEndpointCannotIntroduceTrustedSigner(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(make([]byte, 64))
	message := sha256.Sum256([]byte("openclaw@2026.9.1:" + integrity))
	signature, _ := ecdsa.SignASN1(rand.Reader, key, message[:])
	w := NewWorker()
	keysRequests := 0
	w.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body any
		if req.URL.Path == "/-/npm/v1/keys" {
			keysRequests++
			body = map[string]any{"keys": []any{map[string]any{"keyid": "attacker-key", "key": base64.StdEncoding.EncodeToString(pub)}}}
		} else {
			body = map[string]any{"name": "openclaw", "version": "2026.9.1", "dist": map[string]any{"integrity": integrity, "signatures": []any{map[string]any{"keyid": "attacker-key", "sig": base64.StdEncoding.EncodeToString(signature)}}}}
		}
		b, _ := json.Marshal(body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	})
	if _, err = w.coreRelease(context.Background(), "2026.9.1"); err == nil {
		t.Fatal("accepted a signer introduced only by registry-controlled metadata")
	}
	if keysRequests != 0 {
		t.Fatalf("trust was fetched from registry %d times", keysRequests)
	}
}

func TestProviderMetadataTimestampRejectsMissingStaleAndSkewedDates(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		date    string
		age     string
		wantErr bool
	}{
		{name: "fresh", date: now.Format(http.TimeFormat), age: "0"},
		{name: "missing date", wantErr: true},
		{name: "stale date", date: now.Add(-providerMetadataMaxAge - time.Second).Format(http.TimeFormat), age: "0", wantErr: true},
		{name: "future beyond skew", date: now.Add(providerClockSkewAllowance + time.Second).Format(http.TimeFormat), age: "0", wantErr: true},
		{name: "invalid age", date: now.Format(http.TimeFormat), age: "cached", wantErr: true},
		{name: "stale age", date: now.Format(http.TimeFormat), age: "901", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := make(http.Header)
			if tt.date != "" {
				headers.Set("Date", tt.date)
			}
			if tt.age != "" {
				headers.Set("Age", tt.age)
			}
			_, err := providerMetadataTimestamp(headers, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("providerMetadataTimestamp error=%v wantErr=%t", err, tt.wantErr)
			}
		})
	}

	t.Run("duplicate date fields fail closed", func(t *testing.T) {
		headers := make(http.Header)
		headers.Add("Date", now.Format(http.TimeFormat))
		headers.Add("Date", now.Add(-providerMetadataMaxAge-time.Second).Format(http.TimeFormat))
		if _, err := providerMetadataTimestamp(headers, now); err == nil {
			t.Fatal("accepted ambiguous duplicate Date fields")
		}
	})

	t.Run("duplicate age fields fail closed", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("Date", now.Format(http.TimeFormat))
		headers.Add("Age", "0")
		headers.Add("Age", strconv.FormatInt(int64(providerMetadataMaxAge/time.Second)+1, 10))
		if _, err := providerMetadataTimestamp(headers, now); err == nil {
			t.Fatal("accepted ambiguous duplicate Age fields")
		}
	})
}

func TestProviderMetadataTimestampAgeHeaderPresence(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		ageValues []string
		wantErr   bool
	}{
		{name: "absent age is allowed"},
		{name: "valid age is allowed", ageValues: []string{"0"}},
		{name: "empty age is rejected", ageValues: []string{""}, wantErr: true},
		{name: "whitespace age is rejected", ageValues: []string{" \t "}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("Date", now.Format(http.TimeFormat))
			for _, age := range tt.ageValues {
				headers.Add("Age", age)
			}
			_, err := providerMetadataTimestamp(headers, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("providerMetadataTimestamp error=%v wantErr=%t", err, tt.wantErr)
			}
		})
	}
}

func TestGetFreshMetadataDisablesCachesAndRejectsStaleProviderResponse(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, stale := range []bool{false, true} {
		name := "fresh"
		if stale {
			name = "stale"
		}
		t.Run(name, func(t *testing.T) {
			w := NewWorker()
			w.now = func() time.Time { return now }
			w.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Cache-Control") != "no-cache, no-store" || req.Header.Get("Pragma") != "no-cache" {
					t.Fatalf("provider request did not bypass caches: headers=%v", req.Header)
				}
				date := now
				if stale {
					date = date.Add(-providerMetadataMaxAge - time.Second)
				}
				headers := make(http.Header)
				headers.Set("Date", date.Format(http.TimeFormat))
				headers.Set("Age", "0")
				return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(`{"version":"2026.9.4"}`)), Request: req}, nil
			})}
			data, at, err := w.getFreshMetadata(context.Background(), "https://api.github.com/repos/openclaw/openclaw-windows-node/releases/latest", 1024)
			if stale {
				if err == nil || len(data) != 0 || !at.IsZero() {
					t.Fatalf("stale metadata was accepted: body=%q at=%s err=%v", data, at, err)
				}
				return
			}
			if err != nil || len(data) == 0 || !at.Equal(now.Truncate(time.Second)) {
				t.Fatalf("fresh metadata was rejected: body=%q at=%s err=%v", data, at, err)
			}
		})
	}
}

func TestOfficialCompanionAssetIdentityAndDigest(t *testing.T) {
	version, name := "2026.9.4", "OpenClawCompanion-Setup-x64.exe"
	tag := "v" + version
	url := "https://github.com/openclaw/openclaw-windows-node/releases/download/" + tag + "/" + name
	digest := "sha256:" + hex.EncodeToString(make([]byte, 32))
	if !validCompanionAsset(tag, name, url, digest, name) {
		t.Fatal("rejected canonical official release asset")
	}
	for _, mutation := range []struct{ tag, name, url, digest string }{
		{"v2026.9.4-alpha.1", name, url, digest},
		{tag, name, strings.Replace(url, "openclaw-windows-node", "other-project", 1), digest},
		{tag, name, "https://evil.example/installer.exe", digest},
		{tag, name, url, "sha256:abcd"},
		{tag, "OpenClawCompanion-Setup-arm64.exe", url, digest},
	} {
		if validCompanionAsset(mutation.tag, mutation.name, mutation.url, mutation.digest, name) {
			t.Fatalf("accepted noncanonical asset identity: %+v", mutation)
		}
	}
}

func TestInstallerCacheRejectsMismatchAndNeverOverwritesExistingPath(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "installer.exe")
	content := []byte("official test fixture")
	digest := hashBytes(content)
	if err := writeCachedInstaller(path, content, digest); err != nil {
		t.Fatal(err)
	}
	if got, err := readCachedInstaller(path, digest); err != nil || string(got) != string(content) {
		t.Fatalf("valid cached installer rejected: %q %v", got, err)
	}
	if err := writeCachedInstaller(path, []byte("attacker replacement"), hashBytes([]byte("attacker replacement"))); err == nil {
		t.Fatal("overwrote an existing cached artifact")
	}
	if got, err := readCachedInstaller(path, hashBytes([]byte("attacker replacement"))); err == nil {
		t.Fatalf("accepted a digest mismatch: %q", got)
	}
	if err := writeCachedInstaller(filepath.Join(folder, "bad-digest.exe"), content, hashBytes([]byte("different"))); err == nil {
		t.Fatal("cached content with an incorrect expected digest")
	}
	if _, err := readCachedInstaller(folder, digest); err == nil {
		t.Fatal("accepted a directory as an installer")
	}
	link := filepath.Join(folder, "link.exe")
	if err := os.Symlink(path, link); err == nil {
		if _, err = readCachedInstaller(link, digest); err == nil {
			t.Fatal("followed a symlink in the installer cache")
		}
	}
}

func TestInstallerCacheCorruptionDoesNotBlockFreshVerifiedArtifact(t *testing.T) {
	folder := t.TempDir()
	assetName := "OpenClawCompanion-Setup-x64.exe"
	legacyPath := filepath.Join(folder, assetName)
	corrupt := []byte("partial or corrupted previous download")
	if err := os.WriteFile(legacyPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	content := []byte("new verified installer")
	digest := hashBytes(content)
	if _, _, err := findCachedInstaller(folder, assetName, digest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt cache entry was accepted or surfaced as a fatal cache error: %v", err)
	}
	path, err := newCachedInstallerPath(folder, assetName, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCachedInstaller(path, content, digest); err != nil {
		t.Fatal(err)
	}
	gotPath, got, err := findCachedInstaller(folder, assetName, digest)
	if err != nil || gotPath != path || string(got) != string(content) {
		t.Fatalf("fresh verified cache artifact was not selected: path=%q content=%q err=%v", gotPath, got, err)
	}
	if preserved, err := os.ReadFile(legacyPath); err != nil || string(preserved) != string(corrupt) {
		t.Fatalf("damaged cache evidence was modified: bytes=%q err=%v", preserved, err)
	}
}

func TestInstallerCacheScanBoundsEntryCount(t *testing.T) {
	folder := t.TempDir()
	for i := 0; i <= maxCachedInstallerEntries; i++ {
		path := filepath.Join(folder, fmt.Sprintf("unrelated-%03d", i))
		if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := findCachedInstaller(folder, "OpenClawCompanion-Setup-x64.exe", strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), "too many entries") {
		t.Fatalf("unbounded cache directory was not rejected: %v", err)
	}
}

func TestInstallerCacheScanBoundsBytesAndNormalizesDigest(t *testing.T) {
	folder := t.TempDir()
	assetName := "OpenClawCompanion-Setup-x64.exe"
	content := []byte("verified cached artifact")
	digest := hashBytes(content)
	path, err := newCachedInstallerPath(folder, assetName, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCachedInstaller(path, content, digest); err != nil {
		t.Fatal(err)
	}
	if _, _, err := findCachedInstallerWithByteLimit(folder, assetName, digest, int64(len(content)-1)); !errors.Is(err, errCachedInstallerScanLimit) {
		t.Fatalf("cache scan exceeded its byte budget: %v", err)
	}
	gotPath, got, err := findCachedInstaller(folder, assetName, strings.ToUpper(digest))
	if err != nil || gotPath != path || string(got) != string(content) {
		t.Fatalf("uppercase digest failed to find the same verified cache entry: path=%q bytes=%q err=%v", gotPath, got, err)
	}
}

func TestMaintenanceChildEnvironmentOmitsCredentials(t *testing.T) {
	base := []string{
		"PATH=C:\\Windows\\System32",
		"LOCALAPPDATA=C:\\Users\\tester\\AppData\\Local",
		"HAI_OPENCLAW_MAINTENANCE_TOKEN=maintenance-secret",
		"BACKEND_API_SHARED_KEY=backend-secret",
		"OPENCLAW_GATEWAY_TOKEN=gateway-secret",
		"ANTHROPIC_API_KEY=provider-secret",
		"UNRELATED_SECRET=other-secret",
		"PUBLIC_SETTING=visible",
	}
	child := commandEnvironment(base, []string{"HAI_VERIFY_INSTALLER=C:\\cache\\setup.exe"})
	for _, key := range []string{"PATH", "LOCALAPPDATA", "PUBLIC_SETTING", "HAI_VERIFY_INSTALLER"} {
		found := false
		for _, entry := range child {
			name, _, ok := strings.Cut(entry, "=")
			if ok && strings.EqualFold(name, key) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("safe child environment omitted %s: %v", key, child)
		}
	}
	for _, secret := range []string{"maintenance-secret", "backend-secret", "gateway-secret", "provider-secret", "other-secret"} {
		for _, entry := range child {
			if strings.Contains(entry, secret) {
				t.Errorf("child environment inherited secret %q", secret)
			}
		}
	}
}

type testLockedInstaller struct {
	*strings.Reader
	closeErr   error
	closeCalls int
}

func (f *testLockedInstaller) Close() error {
	f.closeCalls++
	return f.closeErr
}

func TestCompanionInstallerApplyFailsClosedAndCleansUp(t *testing.T) {
	content := "approved installer bytes"
	thumbprint := strings.Repeat("aB", 20)
	base := companionInstallerArtifact{Path: "installer.exe", SHA256: hashBytes([]byte(content)), PublisherThumbprint: thumbprint}

	t.Run("lock failure prevents verification and launch", func(t *testing.T) {
		verified, launched := false, false
		err := applyCompanionInstallerWith(context.Background(), base,
			func(string) (lockedInstallerFile, error) { return nil, os.ErrPermission },
			func(context.Context, string, string) error { verified = true; return nil },
			func(context.Context, string) error { launched = true; return nil })
		if err == nil || verified || launched {
			t.Fatalf("lock failure did not fail closed: err=%v verified=%v launched=%v", err, verified, launched)
		}
	})

	t.Run("partial lock returned with error is closed", func(t *testing.T) {
		file := &testLockedInstaller{Reader: strings.NewReader(content)}
		err := applyCompanionInstallerWith(context.Background(), base,
			func(string) (lockedInstallerFile, error) { return file, os.ErrPermission },
			func(context.Context, string, string) error { return nil },
			func(context.Context, string) error { return nil })
		if err == nil || file.closeCalls != 1 {
			t.Fatalf("partial lock was not cleaned up: err=%v closes=%d", err, file.closeCalls)
		}
	})

	t.Run("digest mismatch closes without signature check or launch", func(t *testing.T) {
		file := &testLockedInstaller{Reader: strings.NewReader("unapproved bytes")}
		verified, launched := false, false
		err := applyCompanionInstallerWith(context.Background(), base,
			func(string) (lockedInstallerFile, error) { return file, nil },
			func(context.Context, string, string) error { verified = true; return nil },
			func(context.Context, string) error { launched = true; return nil })
		if err == nil || verified || launched || file.closeCalls != 1 {
			t.Fatalf("digest failure cleanup incorrect: err=%v verified=%v launched=%v closes=%d", err, verified, launched, file.closeCalls)
		}
	})

	t.Run("signature failure closes without launch", func(t *testing.T) {
		file := &testLockedInstaller{Reader: strings.NewReader(content)}
		launched := false
		err := applyCompanionInstallerWith(context.Background(), base,
			func(string) (lockedInstallerFile, error) { return file, nil },
			func(context.Context, string, string) error { return os.ErrPermission },
			func(context.Context, string) error { launched = true; return nil })
		if err == nil || launched || file.closeCalls != 1 {
			t.Fatalf("signature failure cleanup incorrect: err=%v launched=%v closes=%d", err, launched, file.closeCalls)
		}
	})

	t.Run("execution failure closes lock", func(t *testing.T) {
		file := &testLockedInstaller{Reader: strings.NewReader(content)}
		termination := &ProcessTreeTerminationError{Status: "unknown", cause: ErrProcessTreeTerminationUnverified}
		err := applyCompanionInstallerWith(context.Background(), base,
			func(string) (lockedInstallerFile, error) { return file, nil },
			func(context.Context, string, string) error { return nil },
			func(context.Context, string) error { return termination })
		if err == nil || file.closeCalls != 1 || processTerminationStatus(err) != "unknown" {
			t.Fatalf("execution failure cleanup incorrect: err=%v closes=%d", err, file.closeCalls)
		}
	})

	t.Run("lock close failure rejects apparent success", func(t *testing.T) {
		file := &testLockedInstaller{Reader: strings.NewReader(content), closeErr: os.ErrPermission}
		err := applyCompanionInstallerWith(context.Background(), base,
			func(string) (lockedInstallerFile, error) { return file, nil },
			func(context.Context, string, string) error { return nil },
			func(context.Context, string) error { return nil })
		if err == nil || file.closeCalls != 1 {
			t.Fatalf("close failure was not surfaced: err=%v closes=%d", err, file.closeCalls)
		}
	})

	t.Run("lock remains open through signature and process completion", func(t *testing.T) {
		file := &testLockedInstaller{Reader: strings.NewReader(content)}
		checkHeld := func(stage string) {
			t.Helper()
			if file.closeCalls != 0 {
				t.Fatalf("installer lock was released before %s", stage)
			}
		}
		err := applyCompanionInstallerWith(context.Background(), base,
			func(string) (lockedInstallerFile, error) { return file, nil },
			func(context.Context, string, string) error { checkHeld("Authenticode verification"); return nil },
			func(context.Context, string) error { checkHeld("installer process completion"); return nil })
		if err != nil || file.closeCalls != 1 {
			t.Fatalf("successful apply did not release exactly once: err=%v closes=%d", err, file.closeCalls)
		}
	})
}

func TestValidPublisherThumbprint(t *testing.T) {
	for _, value := range []string{strings.Repeat("aB", 20), strings.Repeat("0", 40), strings.Repeat("g", 40), strings.Repeat("a", 39), strings.Repeat("a", 41)} {
		got := validPublisherThumbprint(value)
		want := value == strings.Repeat("aB", 20) || value == strings.Repeat("0", 40)
		if got != want {
			t.Errorf("validPublisherThumbprint(%q)=%v, want %v", value, got, want)
		}
	}
}

func TestCompanionVersionCheckReportsMissingPublisherPinWithoutDownloadingInstaller(t *testing.T) {
	release := `{"tag_name":"v2026.9.4","prerelease":false,"draft":false,"assets":[{"name":"OpenClawCompanion-Setup-x64.exe","browser_download_url":"https://github.com/openclaw/openclaw-windows-node/releases/download/v2026.9.4/OpenClawCompanion-Setup-x64.exe","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`
	requests := 0
	w := &Worker{
		PublisherThumbprint:       "",
		installedCompanionVersion: func(context.Context) (string, error) { return "2026.6.10", nil },
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.URL.String() != "https://api.github.com/repos/openclaw/openclaw-windows-node/releases/latest" {
				return nil, fmt.Errorf("unexpected download request: %s", req.URL)
			}
			headers := make(http.Header)
			headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))
			headers.Set("Age", "0")
			return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(release)), Request: req}, nil
		})},
	}
	report, artifact, err := w.CompanionCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "ok" || report.Installed != "2026.6.10" || report.Available != "2026.9.4" || report.PublisherVerified || report.PublisherPinStatus != "missing" {
		t.Fatalf("missing publisher pin was treated as ready or hid version discovery: %+v", report)
	}
	if artifact.Path != "" || requests != 1 {
		t.Fatalf("missing pin downloaded or admitted an installer: artifact=%+v requests=%d", artifact, requests)
	}
}

func TestCompanionVersionCheckDoesNotUseStaleProviderMetadata(t *testing.T) {
	requests := 0
	w := NewWorker()
	w.PublisherThumbprint = strings.Repeat("a", 40)
	w.installedCompanionVersion = func(context.Context) (string, error) { return "2026.6.10", nil }
	w.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.String() != "https://api.github.com/repos/openclaw/openclaw-windows-node/releases/latest" {
			return nil, fmt.Errorf("unexpected request to %s", req.URL)
		}
		headers := make(http.Header)
		headers.Set("Date", time.Now().UTC().Add(-providerMetadataMaxAge-time.Second).Format(http.TimeFormat))
		headers.Set("Age", "0")
		return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v2026.9.4","prerelease":false,"draft":false,"assets":[]}`)), Request: req}, nil
	})}
	report, artifact, err := w.CompanionCheck(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale release metadata was accepted: report=%+v err=%v", report, err)
	}
	if report.Installed != "2026.6.10" || report.Available != "" || artifact.Path != "" || requests != 1 {
		t.Fatalf("stale source leaked a candidate or triggered extra work: report=%+v artifact=%+v requests=%d", report, artifact, requests)
	}
}

func TestCompanionVersionCheckDoesNotReportCanceledPublisherVerificationAsMismatch(t *testing.T) {
	const available = "2026.9.4"
	content := []byte("synthetic installer verification fixture")
	digest := hashBytes(content)
	assetName := "OpenClawCompanion-Setup-x64.exe"
	if runtime.GOARCH == "arm64" {
		assetName = "OpenClawCompanion-Setup-arm64.exe"
	}
	assetURL := "https://github.com/openclaw/openclaw-windows-node/releases/download/v" + available + "/" + assetName
	release := fmt.Sprintf(`{"tag_name":"v%s","prerelease":false,"draft":false,"assets":[{"name":%q,"browser_download_url":%q,"digest":"sha256:%s"}]}`,
		available, assetName, assetURL, digest)

	cacheDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", cacheDir)
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	installerDir := filepath.Join(cacheRoot, "HAI", "openclaw-maintenance", available)
	if err := os.MkdirAll(installerDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installerDir, assetName), content, 0600); err != nil {
		t.Fatal(err)
	}

	requests, verifications := 0, 0
	w := NewWorker()
	w.PublisherThumbprint = strings.Repeat("a", 40)
	w.installedCompanionVersion = func(context.Context) (string, error) { return "2026.6.10", nil }
	w.verifyCompanionInstaller = func(context.Context, string, string) error {
		verifications++
		return context.Canceled
	}
	w.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.String() != "https://api.github.com/repos/openclaw/openclaw-windows-node/releases/latest" {
			return nil, fmt.Errorf("unexpected release request: %s", req.URL)
		}
		headers := make(http.Header)
		headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))
		headers.Set("Age", "0")
		return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(release)), Request: req}, nil
	})}

	report, artifact, err := w.CompanionCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "ok" || report.Installed != "2026.6.10" || report.Available != available {
		t.Fatalf("canceled verification hid the discovered version: %+v", report)
	}
	if report.PublisherPinStatus != "configured" || report.PublisherVerified || artifact.Path != "" {
		t.Fatalf("canceled verification was reported as a signer mismatch or installable: report=%+v artifact=%+v", report, artifact)
	}
	w.verifyCompanionInstaller = func(context.Context, string, string) error {
		verifications++
		return errCompanionPublisherMismatch
	}
	report, artifact, err = w.CompanionCheck(context.Background())
	if err != nil || report.PublisherPinStatus != "mismatch" || report.PublisherVerified || artifact.Path != "" {
		t.Fatalf("confirmed signer mismatch was not reported accurately: report=%+v artifact=%+v err=%v", report, artifact, err)
	}
	if requests != 2 || verifications != 2 {
		t.Fatalf("unexpected network or verification calls: requests=%d verifications=%d", requests, verifications)
	}
}

func TestCompanionPostInstallReportPreservesApprovedEvidence(t *testing.T) {
	job := Job{
		Target:   "companion",
		Kind:     "apply",
		Version:  "2026.9.4",
		Evidence: strings.Repeat("a", 64),
	}
	approved := Report{
		Installed:          "2026.6.10",
		Available:          job.Version,
		Evidence:           job.Evidence,
		ProviderMetadataAt: freshTestProviderMetadata(),
		PublisherVerified:  true,
		PublisherPinStatus: "verified",
		ProcessTreeStatus:  "verified",
		Outcome:            "ok",
	}
	// After installation the latest version now equals the installed version,
	// so the read-only check does not download or re-verify the installer.
	postInstall := Report{
		Installed:          job.Version,
		Available:          job.Version,
		Evidence:           strings.Repeat("b", 64),
		ProviderMetadataAt: freshTestProviderMetadata(),
		PublisherPinStatus: "configured",
		Outcome:            "ok",
	}

	got := bindCompanionApplyEvidence(postInstall, approved)
	got.HealthOK = true
	if got.ProcessTreeStatus != "verified" || !got.PublisherVerified || got.PublisherPinStatus != "verified" {
		t.Fatalf("post-install check lost installation evidence: %+v", got)
	}
	if got.Installed != postInstall.Installed || got.Available != postInstall.Available || got.ProviderMetadataAt != postInstall.ProviderMetadataAt {
		t.Fatalf("install proof replaced the post-install version or freshness observation: got=%+v postInstall=%+v", got, postInstall)
	}
	if got.Evidence != approved.Evidence {
		t.Fatalf("receipt was not bound to the approved installer evidence: got=%q want=%q", got.Evidence, approved.Evidence)
	}
	if err := ValidateReport(job, got); err != nil {
		t.Fatalf("verified Companion update receipt was rejected: %v", err)
	}

	for _, tc := range []struct {
		name   string
		change func(*Report)
	}{
		{name: "publisher not verified", change: func(r *Report) { r.PublisherVerified = false }},
		{name: "publisher pin not verified", change: func(r *Report) { r.PublisherPinStatus = "configured" }},
		{name: "process tree not verified", change: func(r *Report) { r.ProcessTreeStatus = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := approved
			tc.change(&invalid)
			report := bindCompanionApplyEvidence(postInstall, invalid)
			report.HealthOK = true
			if err := ValidateReport(job, report); err == nil {
				t.Fatalf("accepted incomplete installation proof: %+v", report)
			}
		})
	}
}

func TestWorkerBlocksGatewayApplyBeforeAnyRuntimeOrNetworkWork(t *testing.T) {
	w := NewWorker()
	_, err := w.Execute(context.Background(), Job{Target: "gateway_core", Kind: "apply", Version: "2026.9.1", Evidence: strings.Repeat("a", 64)})
	if !errors.Is(err, ErrGatewayCoreInstallationBlocked) {
		t.Fatalf("apply error=%v, want WSL-specific containment blocker", err)
	}
}

func TestLoopbackHealthClientBypassesConfiguredProxy(t *testing.T) {
	proxyRequests := make(chan struct{}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests <- struct{}{}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	targetRequests := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		return proxyURL, nil
	}}

	client := newLoopbackHealthClient()
	response, err := client.Get(target.URL)
	if err != nil {
		t.Fatalf("direct loopback health request failed: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("loopback health response status = %d, want 200", response.StatusCode)
	}
	select {
	case <-targetRequests:
	default:
		t.Fatal("health request did not reach the loopback target")
	}
	select {
	case <-proxyRequests:
		t.Fatal("health request escaped through the configured proxy")
	default:
	}
}

func hasEnvironmentValue(environment []string, key, value string) bool {
	for _, entry := range environment {
		name, current, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(name, key) && current == value {
			return true
		}
	}
	return false
}

func TestWorkerRejectsArbitraryTargetsAndReleaseSources(t *testing.T) {
	w := NewWorker()
	if _, err := w.Execute(context.Background(), Job{Target: "cmd.exe", Kind: "apply"}); err == nil {
		t.Fatal("accepted command target")
	}
	for _, address := range []string{"http://registry.npmjs.org/openclaw/latest", "https://example.com/update.exe", "https://registry.npmjs.org.evil.example/update"} {
		if _, err := w.get(context.Background(), address, 1024); err == nil {
			t.Fatalf("accepted %s", address)
		}
	}
	for _, address := range []string{"http://github.com/file", "https://evil.example/file"} {
		req, _ := http.NewRequest("GET", address, nil)
		if w.Client.CheckRedirect(req, nil) == nil {
			t.Fatalf("accepted redirect %s", address)
		}
	}
}

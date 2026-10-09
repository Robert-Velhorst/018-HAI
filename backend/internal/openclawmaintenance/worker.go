package openclawmaintenance

import (
	"bytes"
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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Worker struct {
	Distro, PublisherThumbprint string
	Client                      *http.Client
	BeforeApply                 func() error
	installedCompanionVersion   func(context.Context) (string, error)
	verifyCompanionInstaller    func(context.Context, string, string) error
	registryKeyID               string
	registryPublicKey           string
	now                         func() time.Time
}

type commandPathResolver func(string) (string, error)

func trustedSystemCommandPath(systemDirectory, name string) (string, error) {
	if !filepath.IsAbs(systemDirectory) {
		return "", fmt.Errorf("trusted system directory is unavailable")
	}
	var path string
	switch name {
	case "wsl.exe":
		path = filepath.Join(systemDirectory, "wsl.exe")
	case "powershell.exe":
		path = filepath.Join(systemDirectory, "WindowsPowerShell", "v1.0", "powershell.exe")
	default:
		return "", fmt.Errorf("unsupported maintenance command")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("trusted maintenance executable is unavailable")
	}
	if err := validateTrustedSystemExecutable(path); err != nil {
		return "", fmt.Errorf("trusted maintenance executable is unavailable")
	}
	return filepath.Clean(path), nil
}

// This official npm registry key is pinned independently of package metadata.
// A key rotation intentionally blocks verification until HAI ships a reviewed
// key update; fetching a replacement key from the same registry is not trust.
const (
	npmOpenClawSigningKeyID = "SHA256:DhQ8wR5APBvFHLF/+Tc+AYvPOdTpcIDqOhxsBHRwC7U"
	npmOpenClawSigningKey   = "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEY6Ya7W++7aUPzvMTrezH6Ycx3c+HOKYCcNGybJZSCJq/fd7Qa8uuAKtdIkUQtQiEKERhAmE5lMMJhP8OkDOa2g=="
	maintenanceCommandDelay = 5 * time.Second
	officialNPMRegistry     = "https://registry.npmjs.org/"
)

const maintenanceCommandOutputLimit = 1024 * 1024

func NewWorker() *Worker {
	return &Worker{
		Distro:              "OpenClawGateway",
		PublisherThumbprint: os.Getenv("HAI_OPENCLAW_PUBLISHER_THUMBPRINT"),
		registryKeyID:       npmOpenClawSigningKeyID,
		registryPublicKey:   npmOpenClawSigningKey,
		now:                 func() time.Time { return time.Now().UTC() },
		Client: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || req.URL.Scheme != "https" || (req.URL.Host != "release-assets.githubusercontent.com" && req.URL.Host != "github.com") {
				return fmt.Errorf("untrusted update redirect")
			}
			return nil
		}},
	}
}

// CapabilityHandshake probes the current Windows Job Object path. The backend
// still treats this report as a short-lived authenticated observation, not as
// permission by itself; policy, approval evidence and live permits remain.
func (w *Worker) CapabilityHandshake(ctx context.Context) WorkerCapabilityHandshake {
	capability := unsupportedWorkerCapability()
	if runtime.GOOS != "windows" || probeCompanionProcessContainment(ctx) != nil {
		return capability
	}
	capability.Platform = "windows"
	capability.Supported = true
	capability.Containment = companionJobContainmentID
	capability.CreateSuspended = true
	capability.AssignBeforeResume = true
	capability.KillOnClose = true
	capability.WaitForJobEmpty = true
	capability.UnknownOnUnverifiedExit = true
	return capability
}

type cappedBuffer struct {
	bytes.Buffer
	exceeded bool
	cancel   context.CancelFunc
}

func (b *cappedBuffer) ReadFrom(r io.Reader) (int64, error) {
	// bytes.Buffer promotes ReadFrom; without this override os/exec's io.Copy
	// optimization can bypass Write and grow the embedded buffer without limit.
	return io.Copy(struct{ io.Writer }{Writer: b}, r)
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := maintenanceCommandOutputLimit - b.Len()
	if len(p) > remaining && !b.exceeded {
		b.exceeded = true
		if b.cancel != nil {
			b.cancel()
		}
	}
	if remaining > 0 {
		kept := len(p)
		if kept > remaining {
			kept = remaining
		}
		_, _ = b.Buffer.Write(p[:kept])
	}
	// Continue consuming the pipe after the limit is reached. Returning an error
	// here stops os/exec's reader and can leave a noisy child blocked on stdout.
	return len(p), nil
}

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	return commandWithEnvironment(ctx, name, nil, args...)
}

func commandWithEnvironment(ctx context.Context, name string, overrides []string, args ...string) ([]byte, error) {
	return commandWithResolver(ctx, trustedMaintenanceCommandPath, name, overrides, args...)
}

func commandWithResolver(ctx context.Context, resolve commandPathResolver, name string, overrides []string, args ...string) ([]byte, error) {
	if resolve == nil {
		return nil, fmt.Errorf("trusted maintenance command resolver is unavailable")
	}
	path, err := resolve(name)
	if err != nil || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("trusted maintenance command is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	commandContext, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandContext, path, args...)
	cmd.WaitDelay = maintenanceCommandDelay
	cmd.Env = commandEnvironment(os.Environ(), overrides)
	hideWindow(cmd)
	out := cappedBuffer{cancel: cancel}
	diagnostics := cappedBuffer{cancel: cancel}
	cmd.Stdout = &out
	cmd.Stderr = &diagnostics
	runErr := cmd.Run()
	if out.exceeded || diagnostics.exceeded {
		return nil, fmt.Errorf("maintenance command output limit exceeded")
	}
	if runErr != nil {
		return nil, fmt.Errorf("maintenance command failed")
	}
	return out.Bytes(), nil
}
func (w *Worker) core(ctx context.Context, args ...string) ([]byte, error) {
	return command(ctx, "wsl.exe", append([]string{"--distribution", w.Distro, "--exec", "openclaw"}, args...)...)
}

func coreUpdateCommandArgs(distro, version string) ([]string, error) {
	if strings.TrimSpace(distro) == "" || strings.TrimSpace(distro) != distro || !ValidVersion(version) {
		return nil, fmt.Errorf("unsupported Gateway update target")
	}
	args := []string{"--distribution", distro, "--exec", "env"}
	args = append(args,
		"npm_config_registry="+officialNPMRegistry,
		"NPM_CONFIG_REGISTRY="+officialNPMRegistry,
		"pnpm_config_registry="+officialNPMRegistry,
		"PNPM_CONFIG_REGISTRY="+officialNPMRegistry,
		"BUN_CONFIG_REGISTRY="+officialNPMRegistry,
		"openclaw", "update", "--tag", version, "--json", "--yes", "--timeout", "600",
	)
	return args, nil
}

func (w *Worker) updateCore(ctx context.Context, version string) error {
	args, err := coreUpdateCommandArgs(w.Distro, version)
	if err != nil {
		return err
	}
	_, err = command(ctx, "wsl.exe", args...)
	return err
}

func (w *Worker) nowUTC() time.Time {
	if w.now == nil {
		return time.Now().UTC()
	}
	return w.now().UTC()
}

func providerMetadataTimestamp(headers http.Header, now time.Time) (time.Time, error) {
	dateValues := headers.Values("Date")
	if len(dateValues) != 1 || strings.TrimSpace(dateValues[0]) == "" {
		return time.Time{}, fmt.Errorf("provider metadata has a missing or ambiguous HTTP Date")
	}
	metadataAt, err := http.ParseTime(strings.TrimSpace(dateValues[0]))
	if err != nil {
		return time.Time{}, fmt.Errorf("provider metadata has an invalid HTTP Date")
	}
	if !providerMetadataFresh(&metadataAt, now) {
		return time.Time{}, fmt.Errorf("provider metadata is stale or outside clock-skew tolerance")
	}
	ageValues := headers.Values("Age")
	if len(ageValues) > 1 {
		return time.Time{}, fmt.Errorf("provider metadata has ambiguous HTTP Age")
	}
	if len(ageValues) == 1 {
		ageHeader := strings.TrimSpace(ageValues[0])
		ageSeconds, err := strconv.ParseInt(ageHeader, 10, 64)
		if err != nil || ageSeconds < 0 || ageSeconds > int64(providerMetadataMaxAge/time.Second) {
			return time.Time{}, fmt.Errorf("provider metadata has an invalid or stale HTTP Age")
		}
	}
	return metadataAt.UTC(), nil
}

func (w *Worker) getResponse(ctx context.Context, address string, max int64, requireFreshMetadata bool) ([]byte, *time.Time, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || (u.Host != "registry.npmjs.org" && u.Host != "api.github.com" && u.Host != "github.com") {
		return nil, nil, fmt.Errorf("untrusted release source")
	}
	if w.Client == nil {
		return nil, nil, fmt.Errorf("release client is unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid release request")
	}
	req.Header.Set("User-Agent", "HAI-maintenance/1")
	req.Header.Set("Cache-Control", "no-cache, no-store")
	req.Header.Set("Pragma", "no-cache")
	res, err := w.Client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("release source unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, nil, fmt.Errorf("release source unavailable")
	}
	var metadataAt *time.Time
	if requireFreshMetadata {
		at, err := providerMetadataTimestamp(res.Header, w.nowUTC())
		if err != nil {
			return nil, nil, err
		}
		metadataAt = &at
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if int64(len(data)) > max {
		return nil, nil, fmt.Errorf("release exceeds size bound")
	}
	if err != nil {
		return nil, nil, err
	}
	return data, metadataAt, nil
}

func (w *Worker) get(ctx context.Context, address string, max int64) ([]byte, error) {
	data, _, err := w.getResponse(ctx, address, max, false)
	return data, err
}

func (w *Worker) getFreshMetadata(ctx context.Context, address string, max int64) ([]byte, time.Time, error) {
	data, metadataAt, err := w.getResponse(ctx, address, max, true)
	if err != nil {
		return nil, time.Time{}, err
	}
	if metadataAt == nil {
		return nil, time.Time{}, fmt.Errorf("provider metadata timestamp is unavailable")
	}
	return data, *metadataAt, nil
}
func hashBytes(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func validNPMIntegrity(integrity string) bool {
	if !strings.HasPrefix(integrity, "sha512-") {
		return false
	}
	digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(integrity, "sha512-"))
	return err == nil && len(digest) == 64
}

func (w *Worker) coreRelease(ctx context.Context, version string) (string, error) {
	if !ValidVersion(version) {
		return "", fmt.Errorf("unsupported version")
	}
	data, err := w.get(ctx, "https://registry.npmjs.org/openclaw/"+version, 2*1024*1024)
	if err != nil {
		return "", err
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Dist    struct {
			Integrity  string `json:"integrity"`
			Signatures []struct {
				KeyID string `json:"keyid"`
				Sig   string `json:"sig"`
			} `json:"signatures"`
		} `json:"dist"`
	}
	if json.Unmarshal(data, &pkg) != nil || pkg.Name != "openclaw" || pkg.Version != version || !validNPMIntegrity(pkg.Dist.Integrity) {
		return "", fmt.Errorf("invalid package identity")
	}
	if w.registryKeyID == "" || w.registryPublicKey == "" {
		return "", fmt.Errorf("trusted registry signing key is unavailable")
	}
	der, err := base64.StdEncoding.DecodeString(w.registryPublicKey)
	if err != nil {
		return "", fmt.Errorf("trusted registry signing key is invalid")
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", fmt.Errorf("trusted registry signing key is invalid")
	}
	key, ok := pub.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return "", fmt.Errorf("trusted registry signing key has an unsupported type")
	}
	message := sha256.Sum256([]byte("openclaw@" + version + ":" + pkg.Dist.Integrity))
	for _, sig := range pkg.Dist.Signatures {
		if sig.KeyID != w.registryKeyID {
			continue
		}
		signature, err := base64.StdEncoding.DecodeString(sig.Sig)
		if err == nil && ecdsa.VerifyASN1(key, message[:], signature) {
			return hashBytes([]byte(pkg.Name + "@" + version + ":" + pkg.Dist.Integrity)), nil
		}
	}
	return "", fmt.Errorf("registry signature could not be verified")
}

func (w *Worker) CoreCheck(ctx context.Context) (Report, error) {
	version, err := w.core(ctx, "--version")
	if err != nil {
		return Report{}, err
	}
	installed := ""
	for _, word := range strings.Fields(string(version)) {
		if ValidVersion(word) {
			installed = word
			break
		}
	}
	if installed == "" {
		return Report{}, fmt.Errorf("installed core version unavailable")
	}
	data, err := w.core(ctx, "update", "status", "--json", "--timeout", "10")
	if err != nil {
		return Report{Installed: installed}, err
	}
	var status struct {
		Update struct {
			InstallKind string `json:"installKind"`
		}
		Channel struct{ Value string }
	}
	if json.Unmarshal(data, &status) != nil || status.Update.InstallKind != "package" || status.Channel.Value != "stable" {
		return Report{Installed: installed}, fmt.Errorf("only stable package installs are supported")
	}
	metadata, metadataAt, err := w.getFreshMetadata(ctx, officialNPMRegistry+"openclaw/latest", 2*1024*1024)
	if err != nil {
		return Report{Installed: installed}, err
	}
	var latest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if json.Unmarshal(metadata, &latest) != nil || latest.Name != "openclaw" || !ValidVersion(latest.Version) {
		return Report{Installed: installed}, fmt.Errorf("latest OpenClaw registry metadata is invalid")
	}
	available := latest.Version
	evidence, err := w.coreRelease(ctx, available)
	return Report{Installed: installed, Available: available, Evidence: evidence, ProviderMetadataAt: &metadataAt, PublisherVerified: err == nil, Outcome: "ok"}, err
}

func companionPath() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenClawTray", "OpenClaw.Tray.WinUI.exe")
}
func powershell(ctx context.Context, script string, args ...string) ([]byte, error) {
	return command(ctx, "powershell.exe", append([]string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script}, args...)...)
}

type companionInstallerArtifact struct {
	Path                string
	SHA256              string
	PublisherThumbprint string
}

func (w *Worker) CompanionCheck(ctx context.Context) (Report, companionInstallerArtifact, error) {
	// The script reads a fixed installed file. No dashboard field becomes PowerShell code.
	var version []byte
	var err error
	if w.installedCompanionVersion != nil {
		var installed string
		installed, err = w.installedCompanionVersion(ctx)
		version = []byte(installed)
	} else {
		version, err = powershell(ctx, `(Get-Item -LiteralPath (Join-Path $env:LOCALAPPDATA 'OpenClawTray/OpenClaw.Tray.WinUI.exe')).VersionInfo.ProductVersion`)
	}
	if err != nil {
		return Report{}, companionInstallerArtifact{}, err
	}
	installed := strings.Split(strings.TrimSpace(string(version)), "+")[0]
	if !ValidVersion(installed) {
		return Report{}, companionInstallerArtifact{}, fmt.Errorf("installed Companion version unavailable")
	}
	data, metadataAt, err := w.getFreshMetadata(ctx, "https://api.github.com/repos/openclaw/openclaw-windows-node/releases/latest", 2*1024*1024)
	if err != nil {
		return Report{Installed: installed}, companionInstallerArtifact{}, err
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Prerelease bool
		Draft      bool
		Assets     []struct {
			Name   string
			URL    string `json:"browser_download_url"`
			Digest string
		}
	}
	if json.Unmarshal(data, &release) != nil || release.Prerelease || release.Draft {
		return Report{Installed: installed}, companionInstallerArtifact{}, fmt.Errorf("invalid Companion release")
	}
	available := strings.TrimPrefix(release.Tag, "v")
	if !ValidVersion(available) {
		return Report{Installed: installed}, companionInstallerArtifact{}, fmt.Errorf("unsupported Companion release")
	}
	r := Report{Installed: installed, Available: available, Evidence: hashBytes(data), ProviderMetadataAt: &metadataAt, Outcome: "ok", PublisherPinStatus: companionPublisherPinStatus(w.PublisherThumbprint)}
	if !Newer(available, installed) {
		return r, companionInstallerArtifact{}, nil
	}
	if r.PublisherPinStatus == "missing" || r.PublisherPinStatus == "invalid" {
		// Version discovery remains successful; an update is visible but never
		// represented as publisher-verified without the explicit identity pin.
		return r, companionInstallerArtifact{}, nil
	}
	assetName := "OpenClawCompanion-Setup-x64.exe"
	if runtime.GOARCH == "arm64" {
		assetName = "OpenClawCompanion-Setup-arm64.exe"
	}
	for _, asset := range release.Assets {
		if asset.Name != assetName {
			continue
		}
		if !validCompanionAsset(release.Tag, asset.Name, asset.URL, asset.Digest, assetName) {
			return r, companionInstallerArtifact{}, fmt.Errorf("release asset lacks verified digest")
		}
		cache, err := os.UserCacheDir()
		if err != nil {
			return r, companionInstallerArtifact{}, err
		}
		folder := filepath.Join(cache, "HAI", "openclaw-maintenance", available)
		if err := os.MkdirAll(folder, 0700); err != nil {
			return r, companionInstallerArtifact{}, err
		}
		expected := strings.TrimPrefix(asset.Digest, "sha256:")
		installer, content, err := findCachedInstaller(folder, assetName, expected)
		if errors.Is(err, os.ErrNotExist) {
			content, err = w.get(ctx, asset.URL, 256*1024*1024)
			if err != nil {
				return r, companionInstallerArtifact{}, err
			}
			if hashBytes(content) != expected {
				return r, companionInstallerArtifact{}, fmt.Errorf("installer digest mismatch")
			}
			installer, err = newCachedInstallerPath(folder, assetName, expected)
			if err != nil {
				return r, companionInstallerArtifact{}, err
			}
			if err = writeCachedInstaller(installer, content, expected); err != nil {
				return r, companionInstallerArtifact{}, err
			}
		} else if err != nil {
			return r, companionInstallerArtifact{}, err
		}
		verifyPublisher := w.verifyCompanionInstaller
		if verifyPublisher == nil {
			verifyPublisher = verifyCompanionAuthenticode
		}
		if err := verifyPublisher(ctx, installer, w.PublisherThumbprint); err != nil {
			if errors.Is(err, errCompanionPublisherMismatch) {
				r.PublisherPinStatus = "mismatch"
			}
			return r, companionInstallerArtifact{}, nil
		}
		r.Evidence = hashBytes([]byte(expected + ":" + strings.ToUpper(w.PublisherThumbprint)))
		r.PublisherVerified = true
		r.PublisherPinStatus = "verified"
		return r, companionInstallerArtifact{Path: installer, SHA256: expected, PublisherThumbprint: w.PublisherThumbprint}, nil
	}
	return r, companionInstallerArtifact{}, fmt.Errorf("official installer is missing")
}

const companionAuthenticodeScript = `$ErrorActionPreference = 'Stop'
$signature = Get-AuthenticodeSignature -LiteralPath $env:HAI_VERIFY_INSTALLER
if ($signature.Status -ne 'Valid' -or $null -eq $signature.SignerCertificate) { exit 1 }
[Console]::Out.Write($signature.SignerCertificate.Thumbprint)`

func verifyCompanionAuthenticode(ctx context.Context, installer, expectedThumbprint string) error {
	return verifyCompanionAuthenticodeWith(ctx, installer, expectedThumbprint, trustedMaintenanceCommandPath,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", companionAuthenticodeScript)
}

func verifyCompanionAuthenticodeWith(ctx context.Context, installer, expectedThumbprint string, resolve commandPathResolver, args ...string) error {
	if !validPublisherThumbprint(expectedThumbprint) {
		return fmt.Errorf("installer publisher identity is invalid")
	}
	if resolve == nil {
		return fmt.Errorf("trusted Windows PowerShell is unavailable")
	}
	if len(args) == 0 {
		return fmt.Errorf("trusted Windows PowerShell arguments are unavailable")
	}
	powershellPath, err := resolve("powershell.exe")
	if err != nil || !filepath.IsAbs(powershellPath) {
		return fmt.Errorf("trusted Windows PowerShell is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	commandContext, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandContext, powershellPath, args...)
	cmd.WaitDelay = maintenanceCommandDelay
	cmd.Env = commandEnvironment(os.Environ(), []string{"HAI_VERIFY_INSTALLER=" + installer})
	hideWindow(cmd)
	out := cappedBuffer{cancel: cancel}
	diagnostics := cappedBuffer{cancel: cancel}
	cmd.Stdout = &out
	cmd.Stderr = &diagnostics
	runErr := cmd.Run()
	if out.exceeded || diagnostics.exceeded {
		return fmt.Errorf("installer publisher verification output limit exceeded")
	}
	if runErr != nil {
		return fmt.Errorf("installer publisher verification failed")
	}
	if !strings.EqualFold(strings.TrimSpace(out.String()), expectedThumbprint) {
		return errCompanionPublisherMismatch
	}
	return nil
}

func validPublisherThumbprint(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func companionPublisherPinStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return "missing"
	}
	if !validPublisherThumbprint(value) {
		return "invalid"
	}
	return "configured"
}

type lockedInstallerFile interface {
	io.ReadSeeker
	io.Closer
}

const maxCompanionInstallerSize = 256 * 1024 * 1024

const (
	maxCachedInstallerEntries   = 128
	maxCachedInstallerScanBytes = 2 * maxCompanionInstallerSize
)

var errCompanionPublisherMismatch = errors.New("installer publisher does not match the configured identity pin")
var errCachedInstallerScanLimit = errors.New("installer cache scan exceeds its size bound")

func hashLockedInstaller(file io.ReadSeeker) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("locked installer cannot be read")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, maxCompanionInstallerSize+1))
	if err != nil || n > maxCompanionInstallerSize {
		return "", fmt.Errorf("locked installer cannot be verified")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func applyCompanionInstaller(ctx context.Context, artifact companionInstallerArtifact) error {
	return applyCompanionInstallerWith(ctx, artifact, openCompanionInstallerLocked, verifyCompanionAuthenticode, runCompanionInstaller)
}

func applyCompanionInstallerWith(
	ctx context.Context,
	artifact companionInstallerArtifact,
	open func(string) (lockedInstallerFile, error),
	verify func(context.Context, string, string) error,
	run func(context.Context, string) error,
) (result error) {
	if artifact.Path == "" || !validSHA256Digest("sha256:"+artifact.SHA256) || !validPublisherThumbprint(artifact.PublisherThumbprint) {
		return fmt.Errorf("verified installer identity is incomplete")
	}
	if open == nil || verify == nil || run == nil {
		return fmt.Errorf("verified installer execution is unavailable")
	}
	locked, err := open(artifact.Path)
	if err != nil {
		if locked != nil {
			_ = locked.Close()
		}
		return fmt.Errorf("verified installer could not be locked safely")
	}
	if locked == nil {
		return fmt.Errorf("verified installer could not be locked safely")
	}
	defer func() {
		if closeErr := locked.Close(); closeErr != nil {
			releaseErr := fmt.Errorf("verified installer lock could not be released cleanly")
			if result == nil {
				result = releaseErr
			} else {
				result = errors.Join(result, releaseErr)
			}
		}
	}()

	digest, err := hashLockedInstaller(locked)
	if err != nil || !strings.EqualFold(digest, artifact.SHA256) {
		return fmt.Errorf("locked installer digest does not match the approved release")
	}
	if err := verify(ctx, artifact.Path, artifact.PublisherThumbprint); err != nil {
		return fmt.Errorf("locked installer publisher verification failed")
	}
	if err := run(ctx, artifact.Path); err != nil {
		return fmt.Errorf("verified installer execution failed: %w", err)
	}
	return nil
}

func runCompanionInstaller(ctx context.Context, installer string) error {
	_, err := runContainedCompanionProcess(ctx, installer, []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/SP-"}, commandEnvironment(os.Environ(), nil))
	return err
}

func commandEnvironment(environment, overrides []string) []string {
	overrideKeys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, _ := strings.Cut(entry, "=")
		overrideKeys[strings.ToLower(key)] = struct{}{}
	}
	result := make([]string, 0, len(environment)+len(overrides))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrideKeys[strings.ToLower(key)]; !replaced && !sensitiveChildEnvironmentKey(key) {
			result = append(result, entry)
		}
	}
	return append(result, overrides...)
}

func sensitiveChildEnvironmentKey(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	if strings.HasPrefix(key, "HAI_") || strings.HasPrefix(key, "OPENCLAW_") {
		return true
	}
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "CREDENTIAL", "AUTHORIZATION", "API_KEY", "PRIVATE_KEY"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return strings.HasSuffix(key, "_KEY") || key == "BACKEND_API_SHARED_KEY"
}

func validSHA256Digest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(digest) == sha256.Size
}

func validCompanionAsset(tag, name, address, digest, expectedName string) bool {
	version := strings.TrimPrefix(tag, "v")
	if tag != "v"+version || !ValidVersion(version) || name != expectedName || !validSHA256Digest(digest) {
		return false
	}
	return address == "https://github.com/openclaw/openclaw-windows-node/releases/download/"+tag+"/"+name
}

func readCachedInstaller(path, expected string) ([]byte, error) {
	data, _, err := readCachedInstallerBounded(path, expected, maxCompanionInstallerSize)
	return data, err
}

func readCachedInstallerBounded(path, expected string, byteLimit int64) ([]byte, int64, error) {
	if !validSHA256Digest("sha256:"+expected) || byteLimit < 0 {
		return nil, 0, fmt.Errorf("cached installer identity is invalid")
	}
	if byteLimit > maxCompanionInstallerSize {
		byteLimit = maxCompanionInstallerSize
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("cached installer is not a regular file")
	}
	if info.Size() < 0 || info.Size() > maxCompanionInstallerSize {
		return nil, 0, fmt.Errorf("cached installer exceeds its size bound")
	}
	if info.Size() > byteLimit {
		return nil, 0, errCachedInstallerScanLimit
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("cached installer cannot be opened safely")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, 0, fmt.Errorf("cached installer changed while opening")
	}
	if opened.Size() < 0 || opened.Size() > maxCompanionInstallerSize {
		return nil, 0, fmt.Errorf("cached installer exceeds its size bound")
	}
	if opened.Size() > byteLimit {
		return nil, 0, errCachedInstallerScanLimit
	}
	data, err := io.ReadAll(io.LimitReader(f, byteLimit+1))
	bytesRead := int64(len(data))
	if err != nil {
		return nil, bytesRead, fmt.Errorf("cached installer cannot be read safely")
	}
	if bytesRead > maxCompanionInstallerSize {
		return nil, bytesRead, fmt.Errorf("cached installer exceeds its size bound")
	}
	if bytesRead > byteLimit {
		return nil, bytesRead, errCachedInstallerScanLimit
	}
	if bytesRead != opened.Size() {
		return nil, bytesRead, fmt.Errorf("cached installer changed while reading")
	}
	if !strings.EqualFold(hashBytes(data), expected) {
		return nil, bytesRead, fmt.Errorf("cached installer digest mismatch")
	}
	return data, bytesRead, nil
}

func findCachedInstaller(folder, assetName, expected string) (string, []byte, error) {
	return findCachedInstallerWithByteLimit(folder, assetName, expected, maxCachedInstallerScanBytes)
}

func findCachedInstallerWithByteLimit(folder, assetName, expected string, byteLimit int64) (string, []byte, error) {
	if !validSHA256Digest("sha256:"+expected) || byteLimit < 0 || byteLimit > maxCachedInstallerScanBytes {
		return "", nil, fmt.Errorf("installer cache identity is invalid")
	}
	expected = strings.ToLower(expected)
	directory, err := os.Open(folder)
	if err != nil {
		return "", nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(maxCachedInstallerEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", nil, err
	}
	if len(entries) > maxCachedInstallerEntries {
		return "", nil, fmt.Errorf("installer cache contains too many entries")
	}
	var scanned int64
	for _, entry := range entries {
		name := entry.Name()
		legacy := name == assetName
		versioned := strings.HasPrefix(name, assetName+"-"+expected+"-") && strings.HasSuffix(name, ".exe")
		if !legacy && !versioned {
			continue
		}
		path := filepath.Join(folder, name)
		content, bytesRead, readErr := readCachedInstallerBounded(path, expected, byteLimit-scanned)
		scanned += bytesRead
		if readErr == nil {
			return path, content, nil
		}
		if errors.Is(readErr, errCachedInstallerScanLimit) {
			return "", nil, readErr
		}
		// A damaged or unsafe cache entry is preserved for inspection. It must
		// not prevent HAI from storing a newly verified artifact under a fresh name.
	}
	return "", nil, os.ErrNotExist
}

func newCachedInstallerPath(folder, assetName, expected string) (string, error) {
	if !validSHA256Digest("sha256:"+expected) || filepath.Base(assetName) != assetName || strings.ContainsAny(assetName, `/\\`) {
		return "", fmt.Errorf("installer cache identity is invalid")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("installer cache identity could not be generated")
	}
	name := assetName + "-" + strings.ToLower(expected) + "-" + hex.EncodeToString(nonce[:]) + ".exe"
	return filepath.Join(folder, name), nil
}

func writeCachedInstaller(path string, content []byte, expected string) error {
	if len(content) > maxCompanionInstallerSize || !strings.EqualFold(hashBytes(content), expected) {
		return fmt.Errorf("installer digest mismatch")
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.partial")
	if err != nil {
		return fmt.Errorf("installer cache temporary file could not be created")
	}
	temporaryPath := f.Name()
	linked := false
	defer func() {
		_ = f.Close()
		if !linked {
			_ = os.Remove(temporaryPath)
		}
	}()
	if n, writeErr := f.Write(content); writeErr != nil {
		err = writeErr
	} else if n != len(content) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("installer cache write failed")
	}
	if _, err = readCachedInstaller(temporaryPath, expected); err != nil {
		return err
	}
	// A same-directory hard link publishes the fully synced file atomically and
	// fails if another entry already occupies the destination; it never replaces it.
	if err = os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("installer cache destination already exists or could not be published")
	}
	linked = true
	// The final file is already complete; a leftover temporary hard link is harmless.
	_ = os.Remove(temporaryPath)
	_, err = readCachedInstaller(path, expected)
	return err
}

func bindCompanionApplyEvidence(postInstall, approved Report) Report {
	postInstall.Evidence = approved.Evidence
	postInstall.PublisherVerified = approved.PublisherVerified
	postInstall.PublisherPinStatus = approved.PublisherPinStatus
	postInstall.ProcessTreeStatus = approved.ProcessTreeStatus
	return postInstall
}

func newLoopbackHealthClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (w *Worker) Execute(ctx context.Context, j Job) (Report, error) {
	if !ValidTarget(j.Target) || (j.Kind != "status" && j.Kind != "check" && j.Kind != "apply") {
		return Report{}, fmt.Errorf("unsupported worker job")
	}
	if j.Kind == "apply" && j.Target == "gateway_core" {
		return Report{Evidence: j.Evidence, Outcome: "needs_review"}, ErrGatewayCoreInstallationBlocked
	}
	if j.Kind == "apply" && (j.Target != "companion" || runtime.GOOS != "windows") {
		return Report{Evidence: j.Evidence, Outcome: "needs_review"}, ErrUnattendedInstallationBlocked
	}
	var r Report
	var installer companionInstallerArtifact
	var err error
	if j.Target == "gateway_core" {
		r, err = w.CoreCheck(ctx)
	} else {
		r, installer, err = w.CompanionCheck(ctx)
	}
	if err != nil {
		return r, err
	}
	if j.Kind != "apply" {
		return r, nil
	}
	if j.Version != r.Available || !Newer(j.Version, r.Installed) || !r.PublisherVerified || j.Evidence != r.Evidence {
		return r, fmt.Errorf("release changed or is unverified")
	}
	if w.BeforeApply == nil {
		return r, fmt.Errorf("fresh server authorization is required")
	}
	if err = w.BeforeApply(); err != nil {
		return r, err
	}
	approvedReport := r
	if j.Target == "gateway_core" {
		err = w.updateCore(ctx, j.Version)
	} else {
		err = applyCompanionInstaller(ctx, installer)
	}
	if err != nil {
		r.ProcessTreeStatus = processTerminationStatus(err)
		r.Outcome = "needs_review"
		return r, err
	}
	if j.Target == "companion" {
		r.ProcessTreeStatus = "verified"
		approvedReport.ProcessTreeStatus = "verified"
	}
	verifiedEvidence := r.Evidence
	if j.Target == "gateway_core" {
		r, err = w.CoreCheck(ctx)
	} else {
		var postInstall Report
		postInstall, _, err = w.CompanionCheck(ctx)
		r = bindCompanionApplyEvidence(postInstall, approvedReport)
	}
	if err != nil {
		return r, err
	}
	// Post-install reports bind to the exact artifact admitted before installation,
	// never to a newer release that may appear during the update.
	if j.Target == "gateway_core" {
		r.Evidence = verifiedEvidence
		pinnedEvidence, verifyErr := w.coreRelease(ctx, j.Version)
		if verifyErr != nil || pinnedEvidence != verifiedEvidence {
			r.PublisherVerified = false
			return r, fmt.Errorf("installed package metadata changed")
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:18789/health", nil)
	healthClient := newLoopbackHealthClient()
	res, err := healthClient.Do(req)
	if err != nil {
		return r, err
	}
	defer res.Body.Close()
	var health struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	r.HealthOK = res.StatusCode == 200 && json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&health) == nil && health.OK && health.Status == "live"
	if r.Installed != j.Version || !r.HealthOK {
		r.Outcome = "needs_review"
		return r, fmt.Errorf("post-install verification failed")
	}
	return r, nil
}

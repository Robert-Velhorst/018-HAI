package agentruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	openClawArchiveManifestPrefix = "selection-"
	openClawArchiveManifestSuffix = ".json"
	openClawArchiveStoreName      = ".hai-openclaw-ecosystem"
	openClawArchiveManifestVer    = 1
)

var ErrOpenClawArchiveRollbackUnavailable = errors.New("no integrity-verified previous OpenClaw archive is available for rollback")

type openClawArchiveSelection struct {
	Version        int    `json:"version"`
	Generation     uint64 `json:"generation"`
	SelectedDigest string `json:"selectedDigest"`
	PreviousDigest string `json:"previousDigest,omitempty"`
}

func (a *openClawAdapter) managedArchiveStoreDir() (string, error) {
	root := strings.TrimSpace(a.workspaceRoot)
	if root == "" {
		return "", errors.New("OpenClaw managed archives require a configured persistent workspace root")
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("resolve OpenClaw workspace root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("OpenClaw persistent workspace root is unavailable")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("OpenClaw persistent workspace root cannot be resolved")
	}
	return filepath.Join(root, openClawArchiveStoreName), nil
}

func (a *openClawAdapter) ensureManagedArchiveStoreDir() (string, error) {
	dir, err := a.managedArchiveStoreDir()
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create OpenClaw managed archive store: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("OpenClaw managed archive store is not a regular directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("restrict OpenClaw managed archive store permissions: %w", err)
	}
	return dir, nil
}

func managedOpenClawArchivePath(dir, digest string) (string, error) {
	if !isLowerSHA256(digest) {
		return "", errors.New("OpenClaw managed archive digest is invalid")
	}
	return filepath.Join(dir, "archive-"+digest+".zip"), nil
}

func (a *openClawAdapter) persistManagedArchive(reader io.Reader, digest string, size int64) (string, error) {
	if reader == nil || size <= 0 || size > maxOpenClawEcosystemUploadBytes || !isLowerSHA256(digest) {
		return "", errors.New("OpenClaw managed archive metadata is invalid")
	}
	dir, err := a.ensureManagedArchiveStoreDir()
	if err != nil {
		return "", err
	}
	target, err := managedOpenClawArchivePath(dir, digest)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		if err := verifyManagedOpenClawArchive(target, digest); err != nil {
			return "", err
		}
		return target, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect existing OpenClaw managed archive: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".upload-*.zip")
	if err != nil {
		return "", fmt.Errorf("stage OpenClaw managed archive: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("secure staged OpenClaw archive: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(reader, maxOpenClawEcosystemUploadBytes+1))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written != size || written > maxOpenClawEcosystemUploadBytes ||
		hex.EncodeToString(hash.Sum(nil)) != digest {
		return "", errors.New("uploaded OpenClaw archive failed size or integrity validation")
	}
	if err := validateOpenClawEcosystemPath(tmpPath); err != nil {
		return "", fmt.Errorf("uploaded OpenClaw archive failed safety validation: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		if verifyErr := verifyManagedOpenClawArchive(target, digest); verifyErr != nil {
			return "", fmt.Errorf("persist OpenClaw managed archive: %w", err)
		}
	}
	return target, nil
}

func (a *openClawAdapter) snapshotPreviousArchive(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect currently selected OpenClaw ecosystem: %w", err)
	}
	if info.IsDir() || !strings.EqualFold(filepath.Ext(path), ".zip") {
		return "", nil
	}
	if info.Size() <= 0 || info.Size() > maxOpenClawEcosystemUploadBytes {
		return "", errors.New("currently selected OpenClaw archive is outside the managed archive size limit")
	}
	if err := validateOpenClawEcosystemPath(path); err != nil {
		return "", fmt.Errorf("currently selected OpenClaw archive is no longer valid: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open currently selected OpenClaw archive: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, maxOpenClawEcosystemUploadBytes+1))
	if err != nil || size != info.Size() || size > maxOpenClawEcosystemUploadBytes {
		return "", errors.New("currently selected OpenClaw archive failed integrity validation")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if isManagedOpenClawArchivePath(path, a) {
		if err := verifyManagedOpenClawArchive(path, digest); err != nil {
			return "", err
		}
		return digest, nil
	}
	file, err = os.Open(path)
	if err != nil {
		return "", fmt.Errorf("reopen current OpenClaw archive for retention: %w", err)
	}
	defer file.Close()
	if _, err := a.persistManagedArchive(file, digest, size); err != nil {
		return "", fmt.Errorf("retain previous OpenClaw archive: %w", err)
	}
	return digest, nil
}

func isManagedOpenClawArchivePath(path string, adapter *openClawAdapter) bool {
	dir, err := adapter.managedArchiveStoreDir()
	if err != nil {
		return false
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	if !sameFilePath(filepath.Dir(path), dir) {
		return false
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "archive-") || !strings.HasSuffix(base, ".zip") {
		return false
	}
	digest := strings.TrimSuffix(strings.TrimPrefix(base, "archive-"), ".zip")
	return isLowerSHA256(digest)
}

func verifyManagedOpenClawArchive(path, expectedDigest string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxOpenClawEcosystemUploadBytes {
		return errors.New("OpenClaw managed archive is missing or not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("OpenClaw managed archive cannot be opened")
	}
	hash := sha256.New()
	read, readErr := io.Copy(hash, io.LimitReader(file, maxOpenClawEcosystemUploadBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || read != info.Size() || hex.EncodeToString(hash.Sum(nil)) != expectedDigest {
		return errors.New("OpenClaw managed archive failed SHA-256 integrity validation")
	}
	if err := validateOpenClawEcosystemPath(path); err != nil {
		return fmt.Errorf("OpenClaw managed archive failed safety validation: %w", err)
	}
	return nil
}

func (a *openClawAdapter) loadManagedArchiveSelectionLocked() error {
	if a.managedArchiveSelectionLoaded {
		return nil
	}
	a.managedArchiveSelectionLoaded = true
	dir, err := a.managedArchiveStoreDir()
	if err != nil {
		return nil // A missing persistent root is harmless until HAI-managed uploads are used.
	}
	storeInfo, statErr := os.Lstat(dir)
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	if statErr != nil || !storeInfo.IsDir() || storeInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("OpenClaw managed archive store is not a regular directory")
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read OpenClaw managed archive selection: %w", err)
	}
	var latest string
	var latestGeneration uint64
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, openClawArchiveManifestPrefix) || !strings.HasSuffix(name, openClawArchiveManifestSuffix) {
			continue
		}
		generationText := strings.TrimSuffix(strings.TrimPrefix(name, openClawArchiveManifestPrefix), openClawArchiveManifestSuffix)
		generation, parseErr := strconv.ParseUint(generationText, 10, 64)
		if parseErr == nil && generation > latestGeneration {
			if name != fmt.Sprintf("%s%020d%s", openClawArchiveManifestPrefix, generation, openClawArchiveManifestSuffix) {
				continue
			}
			latest, latestGeneration = filepath.Join(dir, name), generation
		}
	}
	if latest == "" {
		return nil
	}
	manifestInfo, err := os.Lstat(latest)
	if err != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 || manifestInfo.Size() <= 0 || manifestInfo.Size() > 4096 {
		return errors.New("latest OpenClaw managed selection manifest is not a bounded regular file")
	}
	data, err := os.ReadFile(latest)
	if err != nil {
		return fmt.Errorf("read latest OpenClaw managed selection manifest: %w", err)
	}
	var selection openClawArchiveSelection
	if err := json.Unmarshal(data, &selection); err != nil || selection.Version != openClawArchiveManifestVer || selection.Generation != latestGeneration {
		return errors.New("latest OpenClaw managed selection manifest is invalid")
	}
	selectedPath, err := managedOpenClawArchivePath(dir, selection.SelectedDigest)
	if err != nil {
		return err
	}
	if err := verifyManagedOpenClawArchive(selectedPath, selection.SelectedDigest); err != nil {
		return err
	}
	if selection.PreviousDigest != "" {
		previousPath, pathErr := managedOpenClawArchivePath(dir, selection.PreviousDigest)
		if pathErr != nil || verifyManagedOpenClawArchive(previousPath, selection.PreviousDigest) != nil {
			selection.PreviousDigest = ""
			a.managedArchiveWarning = "previous OpenClaw archive failed integrity validation; rollback is unavailable"
		}
	}
	a.managedArchiveSelection = selection
	a.ecosystemPath = selectedPath
	return nil
}

func (a *openClawAdapter) ensureManagedArchiveSelectionLoadedLocked() error {
	if a.managedArchiveSelectionLoaded {
		return nil
	}
	if err := a.loadManagedArchiveSelectionLocked(); err != nil {
		a.managedArchiveLoadError = err.Error()
		a.ecosystemPath = ""
		return err
	}
	return nil
}

func (a *openClawAdapter) writeManagedArchiveSelectionLocked(selectedDigest, previousDigest string) error {
	dir, err := a.ensureManagedArchiveStoreDir()
	if err != nil {
		return err
	}
	if err := verifyManagedOpenClawArchive(mustManagedPath(dir, selectedDigest), selectedDigest); err != nil {
		return err
	}
	if previousDigest != "" {
		if err := verifyManagedOpenClawArchive(mustManagedPath(dir, previousDigest), previousDigest); err != nil {
			return err
		}
	}
	generation := a.managedArchiveSelection.Generation + 1
	if generation == 0 {
		return errors.New("OpenClaw managed selection generation overflow")
	}
	selection := openClawArchiveSelection{
		Version:        openClawArchiveManifestVer,
		Generation:     generation,
		SelectedDigest: selectedDigest,
		PreviousDigest: previousDigest,
	}
	manifestPath := filepath.Join(dir, fmt.Sprintf("%s%020d%s", openClawArchiveManifestPrefix, generation, openClawArchiveManifestSuffix))
	tmp, err := os.CreateTemp(dir, ".selection-*.tmp")
	if err != nil {
		return fmt.Errorf("stage OpenClaw archive selection: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure OpenClaw archive selection: %w", err)
	}
	encodeErr := json.NewEncoder(tmp).Encode(selection)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if encodeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("persist OpenClaw archive selection manifest failed")
	}
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		return fmt.Errorf("activate OpenClaw archive selection manifest: %w", err)
	}
	a.managedArchiveSelection = selection
	a.managedArchiveSelectionLoaded = true
	a.managedArchiveLoadError = ""
	a.managedArchiveWarning = ""
	return nil
}

func mustManagedPath(dir, digest string) string {
	path, _ := managedOpenClawArchivePath(dir, digest)
	return path
}

func (a *openClawAdapter) prepareOpenClawArchiveRollback() (preparedOpenClawEcosystemPath, string, string, error) {
	a.inventoryMu.Lock()
	if err := a.ensureManagedArchiveSelectionLoadedLocked(); err != nil {
		a.inventoryMu.Unlock()
		return preparedOpenClawEcosystemPath{}, "", "", err
	}
	selection := a.managedArchiveSelection
	if selection.SelectedDigest == "" || selection.PreviousDigest == "" {
		a.inventoryMu.Unlock()
		return preparedOpenClawEcosystemPath{}, "", "", ErrOpenClawArchiveRollbackUnavailable
	}
	dir, err := a.managedArchiveStoreDir()
	if err != nil {
		a.inventoryMu.Unlock()
		return preparedOpenClawEcosystemPath{}, "", "", err
	}
	previousPath, err := managedOpenClawArchivePath(dir, selection.PreviousDigest)
	if err != nil {
		a.inventoryMu.Unlock()
		return preparedOpenClawEcosystemPath{}, "", "", err
	}
	if err := verifyManagedOpenClawArchive(previousPath, selection.PreviousDigest); err != nil {
		a.inventoryMu.Unlock()
		return preparedOpenClawEcosystemPath{}, "", "", err
	}
	currentPath := strings.TrimSpace(a.ecosystemPath)
	prepared := preparedOpenClawEcosystemPath{
		targetPath:        previousPath,
		targetSignature:   openClawEcosystemSignature(previousPath),
		previousPath:      currentPath,
		previousSignature: openClawEcosystemSignature(currentPath),
	}
	a.inventoryMu.Unlock()
	return prepared, selection.PreviousDigest, selection.SelectedDigest, nil
}

func (a *openClawAdapter) commitManagedArchiveSelectionLocked(
	prepared preparedOpenClawEcosystemPath,
	selectedDigest string,
	previousDigest string,
) error {
	if err := a.ensureManagedArchiveSelectionLoadedLocked(); err != nil {
		return err
	}
	if openClawEcosystemSignature(prepared.targetPath) != prepared.targetSignature {
		return ErrEcosystemMutationConflict
	}
	currentPath := strings.TrimSpace(a.ecosystemPath)
	if currentPath != prepared.previousPath || openClawEcosystemSignature(currentPath) != prepared.previousSignature {
		return ErrEcosystemMutationConflict
	}
	dir, err := a.managedArchiveStoreDir()
	if err != nil {
		return err
	}
	expectedSelected, err := managedOpenClawArchivePath(dir, selectedDigest)
	if err != nil || !sameFilePath(expectedSelected, prepared.targetPath) {
		return errors.New("OpenClaw selected archive does not match its managed digest")
	}
	if err := a.writeManagedArchiveSelectionLocked(selectedDigest, previousDigest); err != nil {
		return err
	}
	a.ecosystemPath = prepared.targetPath
	a.inventoryLoaded = false
	a.inventoryPath = ""
	a.inventorySignature = ""
	a.inventory = openClawEcosystemInventory{}
	return nil
}

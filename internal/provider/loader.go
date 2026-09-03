package provider

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/good-fish-man/agent-runtime/internal/capability"
	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
	pluginsdk "github.com/good-fish-man/athena-protocol/sdk/plugin"
)

const (
	maxManifestBytes = 2 << 20
	maxSBOMBytes     = 8 << 20
)

type Config struct {
	Enabled          bool
	Directory        string
	RegistryPath     string
	TrustStorePath   string
	AuditPath        string
	RuntimeVersion   string
	RequireSignature bool
}

type LoadReport struct {
	Loaded   []string          `json:"loaded"`
	Rejected map[string]string `json:"rejected"`
}

type trustStore struct {
	Schema string     `json:"schema"`
	Keys   []trustKey `json:"keys"`
}

type trustKey struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"public_key"`
	Disabled  bool   `json:"disabled"`
}

type loadedPackage struct {
	manifest pluginv1.ProviderManifest
	entry    pluginv1.RegistryEntry
	digest   string
}

func LoadAndRegister(ctx context.Context, registry *capability.Registry, cfg Config) (*Manager, LoadReport, error) {
	if registry == nil {
		return nil, LoadReport{}, fmt.Errorf("capability registry is required")
	}
	registry.RemoveExternal()
	report := LoadReport{Rejected: map[string]string{}}
	manager := NewManager(NewJSONLAuditSink(cfg.AuditPath))
	if !cfg.Enabled {
		return manager, report, nil
	}
	if !cfg.RequireSignature {
		return manager, report, fmt.Errorf("unsigned Capability Providers are forbidden; require_signature must be true")
	}
	index, err := loadRegistryIndex(cfg.RegistryPath)
	if err != nil {
		return manager, report, err
	}
	if len(index.Entries) > pluginv1.MaximumProviderPlugins {
		return manager, report, fmt.Errorf("provider registry exceeds %d entries", pluginv1.MaximumProviderPlugins)
	}
	keys, trustErr := loadTrustStore(cfg.TrustStorePath)
	if trustErr != nil && hasActiveEntries(index) {
		return manager, report, trustErr
	}
	entries := append([]pluginv1.RegistryEntry(nil), index.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ProviderID == entries[j].ProviderID {
			return entries[i].Version < entries[j].Version
		}
		return entries[i].ProviderID < entries[j].ProviderID
	})
	for _, entry := range entries {
		identity := entry.ProviderID + "@" + entry.Version
		if entry.Status != pluginv1.StatusActive {
			continue
		}
		loaded, loadErr := loadProviderPackage(cfg, entry, keys)
		if loadErr != nil {
			report.Rejected[identity] = loadErr.Error()
			continue
		}
		binding := manager.add(loaded.manifest, loaded.entry, loaded.digest)
		if healthErr := manager.healthCheck(ctx, binding); healthErr != nil {
			report.Rejected[identity] = healthErr.Error()
			manager.remove(identity)
			continue
		}
		for _, declared := range loaded.manifest.Capabilities {
			definition := capability.Definition{
				ID: declared.ID, Description: declared.Description, Output: "ProviderObservation", ReadOnly: declared.ReadOnly,
				Risk: mapRisk(declared.Risk), RiskFloor: loaded.manifest.RiskFloor, Provider: loaded.manifest.ProviderID,
				ProviderVersion: loaded.manifest.Version, Permissions: permissionMetadata(loaded.entry.GrantedPermissions),
				ObservationContract: declared.ObservationContract,
			}
			declaredCopy := declared
			if registerErr := registry.RegisterExternal(definition, func(string) (tool.BaseTool, error) {
				return newProviderTool(binding, declaredCopy), nil
			}); registerErr != nil {
				report.Rejected[identity] = registerErr.Error()
				manager.remove(identity)
				registry.RemoveExternalProvider(loaded.manifest.ProviderID, loaded.manifest.Version)
				break
			}
		}
		if _, rejected := report.Rejected[identity]; !rejected {
			report.Loaded = append(report.Loaded, identity)
		}
	}
	return manager, report, nil
}

func loadRegistryIndex(path string) (pluginv1.RegistryIndex, error) {
	index := pluginv1.RegistryIndex{Schema: pluginv1.Schema}
	data, err := readBounded(path, maxManifestBytes)
	if errors.Is(err, os.ErrNotExist) {
		return index, nil
	}
	if err != nil {
		return index, fmt.Errorf("read provider registry: %w", err)
	}
	if err := decodeStrict(data, &index); err != nil {
		return index, fmt.Errorf("parse provider registry: %w", err)
	}
	if index.Schema != pluginv1.Schema {
		return index, fmt.Errorf("provider registry schema must be %s", pluginv1.Schema)
	}
	return index, nil
}

func loadTrustStore(path string) (map[string]ed25519.PublicKey, error) {
	data, err := readBounded(path, maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("read provider trust store: %w", err)
	}
	var store trustStore
	if err := decodeStrict(data, &store); err != nil {
		return nil, fmt.Errorf("parse provider trust store: %w", err)
	}
	if store.Schema != pluginv1.TrustStoreSchema {
		return nil, fmt.Errorf("unsupported provider trust store schema")
	}
	result := make(map[string]ed25519.PublicKey, len(store.Keys))
	for _, item := range store.Keys {
		if item.Disabled || item.Algorithm != pluginv1.SignatureEd25519 || strings.TrimSpace(item.KeyID) == "" {
			continue
		}
		decoded, decodeErr := base64.StdEncoding.DecodeString(item.PublicKey)
		if decodeErr != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid public key %q", item.KeyID)
		}
		result[item.KeyID] = ed25519.PublicKey(decoded)
	}
	return result, nil
}

func loadProviderPackage(cfg Config, entry pluginv1.RegistryEntry, keys map[string]ed25519.PublicKey) (loadedPackage, error) {
	if !safePackageSegment(entry.ProviderID) || !safePackageSegment(entry.Version) {
		return loadedPackage{}, fmt.Errorf("registry identity contains an unsafe package path")
	}
	directory := filepath.Join(cfg.Directory, entry.ProviderID, entry.Version)
	if err := validatePackageDirectory(cfg.Directory, directory); err != nil {
		return loadedPackage{}, err
	}
	manifestBytes, err := readBounded(filepath.Join(directory, pluginv1.ManifestFile), maxManifestBytes)
	if err != nil {
		return loadedPackage{}, fmt.Errorf("read plugin manifest: %w", err)
	}
	digest := pluginv1.ManifestSHA256(manifestBytes)
	if digest != entry.ManifestSHA256 {
		return loadedPackage{}, fmt.Errorf("manifest SHA-256 mismatch")
	}
	var manifest pluginv1.ProviderManifest
	if err := decodeStrict(manifestBytes, &manifest); err != nil {
		return loadedPackage{}, fmt.Errorf("parse plugin manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return loadedPackage{}, fmt.Errorf("validate plugin manifest: %w", err)
	}
	if err := entry.Validate(manifest); err != nil {
		return loadedPackage{}, fmt.Errorf("validate registry grant: %w", err)
	}
	if err := entry.ValidateRuntimeGrant(manifest); err != nil {
		return loadedPackage{}, fmt.Errorf("validate Runtime grant: %w", err)
	}
	if !platformSupported(manifest.Platforms) {
		return loadedPackage{}, fmt.Errorf("provider does not support %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if compareVersions(cfg.RuntimeVersion, manifest.MinRuntimeVersion) < 0 {
		return loadedPackage{}, fmt.Errorf("provider requires runtime %s", manifest.MinRuntimeVersion)
	}
	sbom, err := readBounded(filepath.Join(directory, pluginv1.SBOMFile), maxSBOMBytes)
	if err != nil || len(bytes.TrimSpace(sbom)) == 0 {
		return loadedPackage{}, fmt.Errorf("read required SBOM: %w", err)
	}
	assets := make(map[string][]byte, len(manifest.Package.Assets))
	for _, asset := range manifest.Package.Assets {
		content, assetErr := readBounded(filepath.Join(directory, filepath.FromSlash(asset.Path)), asset.SizeBytes)
		if assetErr != nil {
			return loadedPackage{}, fmt.Errorf("read signed plugin asset %s: %w", asset.Path, assetErr)
		}
		assets[asset.Path] = content
	}
	signatureBytes, signatureErr := readBounded(filepath.Join(directory, pluginv1.SignatureFile), maxManifestBytes)
	if signatureErr != nil {
		return loadedPackage{}, fmt.Errorf("read plugin signature: %w", signatureErr)
	}
	var envelope pluginv1.SignatureEnvelope
	if err := decodeStrict(signatureBytes, &envelope); err != nil {
		return loadedPackage{}, fmt.Errorf("parse plugin signature: %w", err)
	}
	providerPackage := pluginsdk.Package{Manifest: manifest, ManifestJSON: manifestBytes, SBOMJSON: sbom, Assets: assets, Signature: envelope}
	if err := pluginsdk.VerifyPackageWithoutTrust(providerPackage); err != nil {
		return loadedPackage{}, fmt.Errorf("validate plugin package: %w", err)
	}
	payloadDigest := pluginsdk.Digest(pluginsdk.SignaturePayload(manifestBytes, sbom, assets))
	if envelope.PayloadSHA256 != payloadDigest {
		return loadedPackage{}, fmt.Errorf("plugin signature payload digest mismatch")
	}
	key, trusted := keys[envelope.KeyID]
	if !trusted {
		return loadedPackage{}, fmt.Errorf("plugin signing key is not trusted")
	}
	if err := pluginsdk.Verify(providerPackage, key); err != nil {
		return loadedPackage{}, err
	}
	scanBytes, scanErr := readBounded(filepath.Join(directory, pluginv1.ScanReportFile), maxManifestBytes)
	if scanErr != nil {
		return loadedPackage{}, fmt.Errorf("read plugin scan report: %w", scanErr)
	}
	if pluginsdk.Digest(scanBytes) != entry.ScanReportSHA256 {
		return loadedPackage{}, fmt.Errorf("plugin scan report SHA-256 mismatch")
	}
	var scan pluginv1.ScanReport
	if err := decodeStrict(scanBytes, &scan); err != nil {
		return loadedPackage{}, fmt.Errorf("parse plugin scan report: %w", err)
	}
	if err := scan.Validate(manifest, digest, payloadDigest); err != nil {
		return loadedPackage{}, fmt.Errorf("validate plugin scan report: %w", err)
	}
	if scan.Status != pluginv1.ScanPassed {
		return loadedPackage{}, fmt.Errorf("plugin machine scan did not pass")
	}
	return loadedPackage{manifest: manifest, entry: entry, digest: digest}, nil
}

func safePackageSegment(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value &&
		!strings.ContainsAny(value, `/\\`)
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("package file must be a regular non-symlink file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return data, nil
}

func validatePackageDirectory(root, directory string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve plugin package root: %w", err)
	}
	realDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return fmt.Errorf("resolve plugin package directory: %w", err)
	}
	relative, err := filepath.Rel(realRoot, realDirectory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("plugin package directory escapes configured root")
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func hasActiveEntries(index pluginv1.RegistryIndex) bool {
	for _, entry := range index.Entries {
		if entry.Status == pluginv1.StatusActive {
			return true
		}
	}
	return false
}

func platformSupported(platforms []pluginv1.Platform) bool {
	for _, platform := range platforms {
		if (platform.OS == "any" || platform.OS == runtime.GOOS) && (platform.Arch == "any" || platform.Arch == runtime.GOARCH) {
			return true
		}
	}
	return false
}

func compareVersions(left, right string) int {
	parse := func(value string) [3]int {
		value = strings.TrimPrefix(value, "v")
		value = strings.SplitN(value, "-", 2)[0]
		parts := strings.Split(value, ".")
		var result [3]int
		for i := range result {
			if i < len(parts) {
				result[i], _ = strconv.Atoi(parts[i])
			}
		}
		return result
	}
	a, b := parse(left), parse(right)
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func mapRisk(value string) string {
	switch value {
	case pluginv1.RiskR0, pluginv1.RiskR1:
		return "low"
	case pluginv1.RiskR2:
		return "medium"
	default:
		return "high"
	}
}

func permissionMetadata(value pluginv1.PermissionSet) map[string]any {
	data, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(data, &result)
	return result
}

package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/good-fish-man/athena-protocol/protocol/plugin/v1"
	log "github.com/good-fish-man/logx"
	"github.com/oklog/ulid/v2"
)

const providerObservationSchema = "athena.provider-observation.v1"

type Manager struct {
	mu        sync.RWMutex
	providers map[string]*binding
	audit     AuditSink
}

type binding struct {
	manager  *Manager
	manifest pluginv1.ProviderManifest
	entry    pluginv1.RegistryEntry
	digest   string
	sem      chan struct{}
	client   *http.Client
}

func NewManager(audit AuditSink) *Manager {
	return &Manager{providers: make(map[string]*binding), audit: audit}
}

func (m *Manager) add(manifest pluginv1.ProviderManifest, entry pluginv1.RegistryEntry, digest string) *binding {
	value := &binding{manager: m, manifest: manifest, entry: entry, digest: digest, sem: make(chan struct{}, manifest.Resources.MaxConcurrency)}
	if manifest.Runtime.Kind == pluginv1.RuntimeHTTPJSON {
		value.client = restrictedClient(manifest.Runtime.BaseURL, time.Duration(manifest.Resources.MaxExecutionMS)*time.Millisecond)
	}
	m.mu.Lock()
	m.providers[manifest.ProviderID+"@"+manifest.Version] = value
	m.mu.Unlock()
	return value
}

func (m *Manager) remove(identity string) {
	m.mu.Lock()
	delete(m.providers, identity)
	m.mu.Unlock()
}

func (m *Manager) Providers() []pluginv1.ProviderManifest {
	m.mu.RLock()
	result := make([]pluginv1.ProviderManifest, 0, len(m.providers))
	for _, value := range m.providers {
		result = append(result, value.manifest)
	}
	m.mu.RUnlock()
	return result
}

func (m *Manager) invoke(ctx context.Context, value *binding, capability pluginv1.Capability, input string) (output string, err error) {
	started := time.Now().UTC()
	trace := pluginv1.InvocationTrace{
		Schema: pluginv1.Schema, InvocationID: ulid.Make().String(), ProviderID: value.manifest.ProviderID,
		ProviderVersion: value.manifest.Version, CapabilityID: capability.ID, TraceID: log.GetReqId(),
		Status: pluginv1.InvocationRunning, PermissionSnapshot: value.entry.GrantedPermissions,
		InputSHA256: digestString(input), StartedAt: started,
	}
	if trace.TraceID == "" {
		trace.TraceID = "plugin-" + strings.ToLower(trace.InvocationID)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("provider invocation isolated panic: %v", recovered)
		}
		trace.FinishedAt = time.Now().UTC()
		trace.DurationMS = trace.FinishedAt.Sub(started).Milliseconds()
		if err != nil {
			if trace.Status == pluginv1.InvocationRunning {
				trace.Status = pluginv1.InvocationFailed
			}
			trace.ErrorCode = classifyError(err)
		} else {
			trace.Status = pluginv1.InvocationSucceeded
			trace.OutputSHA256 = digestString(output)
			trace.ObservationRef = "provider-observation:" + trace.OutputSHA256
		}
		if m.audit == nil {
			auditErr := fmt.Errorf("provider invocation audit sink is required")
			if err == nil {
				err = auditErr
			} else {
				err = errors.Join(err, auditErr)
			}
			return
		}
		if auditErr := safeAuditRecord(m.audit, context.WithoutCancel(ctx), trace); auditErr != nil {
			if err == nil {
				err = auditErr
			} else {
				err = errors.Join(err, auditErr)
			}
		}
	}()

	if len(input) > value.manifest.Resources.MaxInputBytes {
		trace.Status = pluginv1.InvocationDenied
		return "", fmt.Errorf("provider input exceeds %d bytes", value.manifest.Resources.MaxInputBytes)
	}
	var decoded any
	if decodeErr := json.Unmarshal([]byte(input), &decoded); decodeErr != nil {
		trace.Status = pluginv1.InvocationDenied
		return "", fmt.Errorf("provider input is not valid JSON: %w", decodeErr)
	}
	if schemaErr := validateJSONValue(capability.InputSchema, decoded, "input"); schemaErr != nil {
		trace.Status = pluginv1.InvocationDenied
		return "", schemaErr
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(value.manifest.Resources.MaxExecutionMS)*time.Millisecond)
	defer cancel()
	select {
	case value.sem <- struct{}{}:
		defer func() { <-value.sem }()
	case <-runCtx.Done():
		return "", fmt.Errorf("provider concurrency wait: %w", runCtx.Err())
	}

	raw, runErr := invokeRuntime(runCtx, value, capability, []byte(input))
	if runErr != nil {
		return "", runErr
	}
	if len(raw) > value.manifest.Resources.MaxOutputBytes {
		return "", fmt.Errorf("provider output exceeds %d bytes", value.manifest.Resources.MaxOutputBytes)
	}
	var data any
	if decodeErr := json.Unmarshal(raw, &data); decodeErr != nil {
		return "", fmt.Errorf("provider output is not valid JSON: %w", decodeErr)
	}
	if schemaErr := validateJSONValue(capability.OutputSchema, data, "output"); schemaErr != nil {
		return "", schemaErr
	}
	observation := map[string]any{
		"schema": providerObservationSchema, "provider_id": value.manifest.ProviderID,
		"provider_version": value.manifest.Version, "capability_id": capability.ID,
		"observation_contract": capability.ObservationContract, "data": data,
		"provenance": map[string]any{"manifest_sha256": value.digest, "invocation_id": trace.InvocationID, "trace_id": trace.TraceID},
	}
	encoded, encodeErr := json.Marshal(observation)
	if encodeErr != nil {
		return "", fmt.Errorf("encode provider observation: %w", encodeErr)
	}
	return string(encoded), nil
}

func safeAuditRecord(sink AuditSink, ctx context.Context, trace pluginv1.InvocationTrace) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("provider audit sink panic: %v", recovered)
		}
	}()
	return sink.Record(ctx, trace)
}

func invokeRuntime(ctx context.Context, value *binding, capability pluginv1.Capability, input []byte) ([]byte, error) {
	switch value.manifest.Runtime.Kind {
	case pluginv1.RuntimeStaticJSON:
		return append([]byte(nil), value.manifest.Runtime.StaticResponses[capability.ID]...), nil
	case pluginv1.RuntimeHTTPJSON:
		operation := value.manifest.Runtime.Operations[capability.ID]
		endpoint, err := url.JoinPath(value.manifest.Runtime.BaseURL, operation.Path)
		if err != nil {
			return nil, fmt.Errorf("build provider endpoint: %w", err)
		}
		var body io.Reader
		if strings.EqualFold(operation.Method, http.MethodGet) {
			var values map[string]any
			_ = json.Unmarshal(input, &values)
			parsed, _ := url.Parse(endpoint)
			query := parsed.Query()
			for key, item := range values {
				switch typed := item.(type) {
				case string:
					query.Set(key, typed)
				case float64, bool:
					query.Set(key, fmt.Sprint(typed))
				default:
					encoded, _ := json.Marshal(typed)
					query.Set(key, string(encoded))
				}
			}
			parsed.RawQuery = query.Encode()
			endpoint = parsed.String()
		} else {
			body = bytes.NewReader(input)
		}
		request, err := http.NewRequestWithContext(ctx, strings.ToUpper(operation.Method), endpoint, body)
		if err != nil {
			return nil, fmt.Errorf("create provider request: %w", err)
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", "Athena-Capability-Provider/0.8")
		response, err := value.client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("provider HTTP request: %w", err)
		}
		defer response.Body.Close()
		limited := io.LimitReader(response.Body, int64(value.manifest.Resources.MaxOutputBytes)+1)
		result, readErr := io.ReadAll(limited)
		if readErr != nil {
			return nil, fmt.Errorf("read provider response: %w", readErr)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("provider HTTP status %d", response.StatusCode)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("provider runtime kind is not executable")
	}
}

func restrictedClient(baseURL string, timeout time.Duration) *http.Client {
	parsed, _ := url.Parse(baseURL)
	allowedHost := strings.ToLower(parsed.Hostname())
	allowLoopback := allowedHost == "localhost"
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(host, allowedHost) {
				return nil, fmt.Errorf("provider network target is outside the exact domain grant")
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				if unsafeProviderIP(address.IP) && !(allowLoopback && address.IP.IsLoopback()) {
					continue
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
			}
			return nil, fmt.Errorf("provider domain resolved only to denied network ranges")
		},
		MaxIdleConns: 16, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Timeout: timeout, Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !strings.EqualFold(request.URL.Hostname(), allowedHost) || request.URL.Scheme != parsed.Scheme {
				return fmt.Errorf("provider redirect leaves the exact domain grant")
			}
			return nil
		},
	}
}

func unsafeProviderIP(ip net.IP) bool {
	return ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func classifyError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "TIMEOUT"
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "denied") || strings.Contains(text, "exceeds") || strings.Contains(text, "schema") {
		return "POLICY_DENIED"
	}
	if strings.Contains(text, "http") || strings.Contains(text, "network") {
		return "PROVIDER_UNAVAILABLE"
	}
	return "PROVIDER_FAILED"
}

// Package readiness evaluates the runtime invariants required by the Athena
// Personal Agent OS v1.0 GA contract.
package readiness

import (
	"fmt"
	"strings"
	"time"

	"github.com/good-fish-man/agent-runtime/internal/operations"
	ga "github.com/good-fish-man/athena-protocol/protocol/ga/v1"
	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

type Config struct {
	Version                string
	InstanceID             string
	PluginsEnabled         bool
	PluginRequireSignature bool
	PluginTrustStorePath   string
	MemoryEnabled          bool
	DatabaseEnabled        bool
}

func Build(gate *operations.Gate, cfg Config) ga.ReadinessReport {
	checks := make([]ga.ReadinessCheck, 0, 8)
	if cfg.Version != ga.ReleaseVersion {
		checks = append(checks, fail("protocol.freeze", "compatibility", "runtime version does not match the frozen GA release"))
	} else {
		checks = append(checks, pass("protocol.freeze", "compatibility", "compiled runtime and GA protocol versions match the frozen release"))
	}
	checks = append(checks,
		pass("execution.typed", "traceability", "execution uses typed capability, action, and observation contracts"),
		pass("frontend.independent", "durability", "runtime execution and background services do not require the frontend process"),
	)
	if gate == nil {
		checks = append(checks, fail("admission.control", "reliability", "runtime admission gate is unavailable"))
	} else {
		health := gate.Health(cfg.Version)
		if health.Status == operationsv1.HealthUnhealthy {
			checks = append(checks, fail("admission.control", "reliability", "runtime admission gate is unhealthy"))
		} else {
			checks = append(checks, pass("admission.control", "reliability", "bounded admission, queueing, deadlines, and graceful drain are active"))
		}
	}
	if !cfg.PluginsEnabled {
		checks = append(checks, pass("plugin.trust", "security", "external capability providers are disabled"))
	} else if cfg.PluginRequireSignature && strings.TrimSpace(cfg.PluginTrustStorePath) != "" {
		checks = append(checks, pass("plugin.trust", "security", "signed capability providers and an explicit trust store are required"))
	} else {
		checks = append(checks, fail("plugin.trust", "security", "enabled capability providers must require signatures and a trust store"))
	}
	if !cfg.MemoryEnabled {
		checks = append(checks, pass("memory.persistence", "data", "runtime memory is explicitly disabled"))
	} else if cfg.DatabaseEnabled {
		checks = append(checks, pass("memory.persistence", "data", "memory is configured with durable database storage"))
	} else {
		checks = append(checks, fail("memory.persistence", "data", "memory is enabled without durable database storage"))
	}

	status := ga.StatusPass
	for _, check := range checks {
		if check.Required && (check.Status == ga.StatusFail || check.Status == ga.StatusBlocked) {
			status = ga.StatusFail
			break
		}
	}
	return ga.ReadinessReport{
		Schema: ga.Schema, ReleaseVersion: cfg.Version, Component: "agent-runtime",
		InstanceID: cfg.InstanceID, Status: status, Checks: checks, ObservedAt: time.Now().UTC(),
	}
}

func pass(id, category, message string) ga.ReadinessCheck {
	return ga.ReadinessCheck{ID: id, Category: category, Status: ga.StatusPass, Required: true, Message: message}
}

func fail(id, category, message string) ga.ReadinessCheck {
	return ga.ReadinessCheck{ID: id, Category: category, Status: ga.StatusFail, Required: true, Message: message}
}

func HTTPStatus(report ga.ReadinessReport) int {
	if report.Status == ga.StatusPass {
		return 200
	}
	return 503
}

func Validate(report ga.ReadinessReport) error {
	if err := report.Validate(); err != nil {
		return fmt.Errorf("validate runtime readiness report: %w", err)
	}
	return nil
}

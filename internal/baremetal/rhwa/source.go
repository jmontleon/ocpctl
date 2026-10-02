package rhwa

import (
	"context"
	"fmt"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/agent"
)

// Deploy-from-source ("make dev-olm-deploy") support, ported from rhwa-lab's
// RHWA_INSTALL_METHOD=make. Each supported operator is built from its upstream
// medik8s repo on the host (operator + OLM bundle images), pushed to a registry
// (ttl.sh by default), and installed via `operator-sdk run bundle` so OLM
// manages it. Operators without the dev.mk flow — and any build that fails —
// fall back to the OLM catalog Subscription.

const (
	defaultDevRegistry = "ttl.sh" // public, ephemeral (~24h) — fine for a short-lived lab
	defaultToolsDir    = agent.RemoteWorkDir + "/rhwa-tools"
	bundleTimeout      = "10m" // operator-sdk run bundle (default 2m is too short on nested virt)
	medik8sBase        = "https://github.com/medik8s"
)

// opSource is an operator's deploy-from-source capability + upstream repo.
type opSource struct {
	canMake bool   // has a tools/dev.mk dev-olm-deploy flow
	repo    string // upstream git repo
}

// opDefaults maps the RHWA operator set to its source build capability. Mirrors
// rhwa-lab's _op_defaults: all medik8s operators build from source except
// machine-deletion-remediation, which has no dev.mk flow (catalog only).
var opDefaults = map[string]opSource{
	"node-healthcheck-operator":    {true, medik8sBase + "/node-healthcheck-operator"},
	"fence-agents-remediation":     {true, medik8sBase + "/fence-agents-remediation"},
	"self-node-remediation":        {true, medik8sBase + "/self-node-remediation"},
	"node-maintenance-operator":    {true, medik8sBase + "/node-maintenance-operator"},
	"machine-deletion-remediation": {false, medik8sBase + "/machine-deletion-remediation"},
	"storage-based-remediation":    {true, medik8sBase + "/storage-based-remediation"},
}

// opBuildsFromSource reports whether the operator should be built from source
// (its own dev.mk flow); unknown operators fall back to the catalog.
func opBuildsFromSource(name string) (repo string, ok bool) {
	d, found := opDefaults[name]
	if !found || !d.canMake {
		return "", false
	}
	return d.repo, true
}

// isNHC reports the node-healthcheck-operator, whose bundle build additionally
// needs CONSOLE_PLUGIN_IMAGE / MUST_GATHER_IMAGE digests (resolved in-script).
func isNHC(name string) bool { return name == "node-healthcheck-operator" }

func (s Spec) devRegistry() string {
	if s.DevRegistry != "" {
		return s.DevRegistry
	}
	return defaultDevRegistry
}

// installFromSource ports rhwa-lab's make path: prep the host toolchain + tools
// repo once, then install each operator from source (catalog fallback per
// operator), keeping the namespace on Automatic install-plan approval throughout.
func installFromSource(ctx context.Context, r runner, spec Spec, sleep func(time.Duration)) error {
	ns, err := renderNamespace(spec)
	if err != nil {
		return err
	}
	if err := r.Run(ctx, ns); err != nil {
		return fmt.Errorf("rhwa ensure namespace: %w", err)
	}

	prep, err := renderPrepare(spec)
	if err != nil {
		return err
	}
	if err := r.Run(ctx, prep); err != nil {
		return fmt.Errorf("rhwa dev-olm prepare host: %w", err)
	}

	for _, op := range spec.Operators {
		repo, fromSource := opBuildsFromSource(op.Name)
		if fromSource {
			script, rerr := renderMakeOp(spec, op, repo)
			if rerr != nil {
				return rerr
			}
			if err := r.Run(ctx, script); err != nil {
				// dev-olm-deploy failed (the script already undeployed any partial
				// install); fall back to the OLM catalog for this operator.
				cat, cerr := renderCatalogOp(spec, op)
				if cerr != nil {
					return cerr
				}
				if err := r.Run(ctx, cat); err != nil {
					return fmt.Errorf("rhwa catalog fallback %s: %w", op.Name, err)
				}
			}
		} else {
			cat, cerr := renderCatalogOp(spec, op)
			if cerr != nil {
				return cerr
			}
			if err := r.Run(ctx, cat); err != nil {
				return fmt.Errorf("rhwa catalog install %s: %w", op.Name, err)
			}
		}
		// Keep the namespace Automatic so the next operator's install isn't stalled
		// by this one's pending (Manual) install plan.
		if approve, aerr := renderAutoApprove(spec); aerr == nil {
			_ = r.Run(ctx, approve)
		}
	}

	if approve, aerr := renderAutoApprove(spec); aerr == nil {
		_ = r.Run(ctx, approve) // final sweep for any plan that just appeared
	}
	for _, op := range spec.Operators {
		waitCSV(ctx, r, spec.ns(), op.Name, sleep)
	}
	return nil
}

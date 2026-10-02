package rhwa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sourceSpec() Spec {
	s := testSpec()
	s.FromSource = true
	s.Operators = testOperators() // NHC, FAR, SNR — all build from source
	return s
}

func TestOpBuildsFromSource(t *testing.T) {
	for _, name := range []string{
		"node-healthcheck-operator", "fence-agents-remediation",
		"self-node-remediation", "node-maintenance-operator", "storage-based-remediation",
	} {
		repo, ok := opBuildsFromSource(name)
		assert.True(t, ok, name)
		assert.Contains(t, repo, "github.com/medik8s/"+name)
	}
	// MDR has no dev.mk flow -> catalog only; unknown -> catalog.
	_, ok := opBuildsFromSource("machine-deletion-remediation")
	assert.False(t, ok)
	_, ok = opBuildsFromSource("something-else")
	assert.False(t, ok)
}

func TestRenderMakeOp(t *testing.T) {
	far := Operator{Name: "fence-agents-remediation"}
	out, err := renderMakeOp(sourceSpec(), far, "https://github.com/medik8s/fence-agents-remediation")
	require.NoError(t, err)
	assert.Contains(t, out, "git clone --depth 1 'https://github.com/medik8s/fence-agents-remediation'")
	assert.Contains(t, out, "make dev-olm-deploy")
	assert.Contains(t, out, "CONTAINER_TOOL=podman")
	assert.Contains(t, out, "DEV_REGISTRY=ttl.sh") // default registry
	assert.Contains(t, out, "make dev-olm-undeploy")
	assert.NotContains(t, out, "CONSOLE_PLUGIN_IMAGE") // only NHC resolves digests

	// NHC additionally resolves its related-image digests in-script.
	nhc := Operator{Name: "node-healthcheck-operator"}
	nout, err := renderMakeOp(sourceSpec(), nhc, "https://github.com/medik8s/node-healthcheck-operator")
	require.NoError(t, err)
	assert.Contains(t, nout, "image info")
	assert.Contains(t, nout, "CONSOLE_PLUGIN_IMAGE=")
	assert.Contains(t, nout, "MUST_GATHER_IMAGE=")

	// A custom registry overrides the ttl.sh default.
	s := sourceSpec()
	s.DevRegistry = "registry.example.com/rhwa"
	cout, _ := renderMakeOp(s, far, "https://github.com/medik8s/fence-agents-remediation")
	assert.Contains(t, cout, "DEV_REGISTRY=registry.example.com/rhwa")
}

func TestInstallFromSource_Orchestration(t *testing.T) {
	f := &fakeRunner{capture: func(string) (string, error) { return "Succeeded", nil }}
	require.NoError(t, installFromSource(context.Background(), f, sourceSpec(), func(time.Duration) {}))

	assert.Equal(t, 1, f.ranContaining("kind: OperatorGroup"))      // namespace once
	assert.Equal(t, 1, f.ranContaining("dnf install"))              // host prep once
	assert.Equal(t, 3, f.ranContaining("make dev-olm-deploy"))      // one per operator
	assert.Equal(t, 0, f.ranContaining("kind: Subscription"))       // no fallback needed
	assert.GreaterOrEqual(t, f.ranContaining("get installplan"), 3) // auto-approve sweeps
}

func TestInstallFromSource_CatalogFallback(t *testing.T) {
	// Every make build "fails" -> each operator must fall back to a catalog sub.
	f := &fakeRunner{
		capture: func(string) (string, error) { return "Succeeded", nil },
		runHook: func(s string) error {
			if strings.Contains(s, "make dev-olm-deploy") {
				return assertErr
			}
			return nil
		},
	}
	require.NoError(t, installFromSource(context.Background(), f, sourceSpec(), func(time.Duration) {}))
	assert.Equal(t, 3, f.ranContaining("make dev-olm-deploy")) // attempted each
	assert.Equal(t, 3, f.ranContaining("kind: Subscription"))  // fell back each
}

type constErr string

func (e constErr) Error() string { return string(e) }

const assertErr = constErr("simulated make failure")

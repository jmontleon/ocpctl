package rhwa

import (
	"bytes"
	"embed"
	"text/template"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/agent"
)

//go:embed templates/*.sh.tmpl
var templatesFS embed.FS

var tmpls = template.Must(template.ParseFS(templatesFS, "templates/*.sh.tmpl"))

func render(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type opsData struct {
	OC        string
	NS        string
	Operators []Operator
}

type fenceNode struct {
	Host string
	UUID string
}

type fenceData struct {
	OC         string
	NS         string
	NetGateway string
	SushyPort  int
	SushyUser  string
	SushyPass  string
	FenceNodes []fenceNode
}

func renderOperators(spec Spec) (string, error) {
	return render("operators.sh.tmpl", opsData{
		OC:        ocCmd,
		NS:        spec.ns(),
		Operators: spec.Operators,
	})
}

// --- deploy-from-source (make dev-olm-deploy) renderers ---

type namespaceData struct{ OC, NS string }

type prepareData struct {
	ToolsDir      string
	BundleTimeout string
}

type makeOpData struct {
	RawOC      string // plain oc (the make runs as root, no sudo wrapper)
	Kubeconfig string
	NS         string
	Op         string
	Repo       string
	Registry   string
	ToolsDir   string
	IsNHC      bool
}

type catalogOpData struct {
	OC                    string
	NS                    string
	Name, Channel, Source string
}

func renderNamespace(spec Spec) (string, error) {
	return render("namespace.sh.tmpl", namespaceData{OC: ocCmd, NS: spec.ns()})
}

func renderPrepare(spec Spec) (string, error) {
	return render("dev-olm-prepare.sh.tmpl", prepareData{ToolsDir: defaultToolsDir, BundleTimeout: bundleTimeout})
}

func renderMakeOp(spec Spec, op Operator, repo string) (string, error) {
	return render("make-operator.sh.tmpl", makeOpData{
		RawOC:      agent.RemoteOC,
		Kubeconfig: agent.RemoteKubeconfig,
		NS:         spec.ns(),
		Op:         op.Name,
		Repo:       repo,
		Registry:   spec.devRegistry(),
		ToolsDir:   defaultToolsDir,
		IsNHC:      isNHC(op.Name),
	})
}

func renderCatalogOp(spec Spec, op Operator) (string, error) {
	return render("operator-catalog.sh.tmpl", catalogOpData{
		OC: ocCmd, NS: spec.ns(), Name: op.Name, Channel: op.Channel, Source: op.Source,
	})
}

func renderAutoApprove(spec Spec) (string, error) {
	return render("auto-approve.sh.tmpl", namespaceData{OC: ocCmd, NS: spec.ns()})
}

func renderFencing(spec Spec) (string, error) {
	var nodes []fenceNode
	for _, n := range clusterNodes(spec.Nodes) {
		nodes = append(nodes, fenceNode{Host: n.Host, UUID: spec.UUIDs[n.Name]})
	}
	return render("fencing.sh.tmpl", fenceData{
		OC:         ocCmd,
		NS:         spec.ns(),
		NetGateway: spec.NetGateway,
		SushyPort:  spec.SushyPort,
		SushyUser:  spec.SushyUser,
		SushyPass:  spec.SushyPass,
		FenceNodes: nodes,
	})
}

package helheim

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ouroboric-dev/gungnir"
	"github.com/ouroboric-dev/gungnir/schema"
)

func TestThePackLoadsAndOffersTheRecipeEveryBuildRealmOffers(t *testing.T) {
	p, err := schema.LoadPack(".")
	if err != nil {
		t.Fatalf("LoadPack() = %v", err)
	}
	if p.Name != "helheim-go" || p.Graph != "" {
		t.Errorf("name %q, graph %q; helheim-go provides leaves and has no graph in v0", p.Name, p.Graph)
	}
	r := p.Leaf
	if r == nil || r.Template != "templates/api" || r.Implementer != "prompts/implementer.md" {
		t.Fatalf("leaf recipe = %+v", r)
	}
	want := []string{"static", "unit", "tdd", "contract", "deps", "security", "integration"}
	if strings.Join(r.Checks, ",") != strings.Join(want, ",") {
		t.Errorf("the ladder is %v, want %v: cheap rungs first, the one that needs services last", r.Checks, want)
	}
	ref, err := p.StackRef(".")
	if err != nil || !regexp.MustCompile(`^localhost/ygg-stack-helheim-go:[0-9a-f]{12}$`).MatchString(ref) {
		t.Errorf("StackRef() = %q, %v; the stack is built from devcontainer/", ref, err)
	}
	if !gungnir.ValidStack(ref) {
		t.Errorf("%q is not a stack a sandbox can start on", ref)
	}
	for _, h := range p.Sandbox.Egress {
		if !gungnir.ValidHost(h) {
			t.Errorf("egress %q is not a plain host", h)
		}
	}
}

func TestEveryCheckRunsAScriptThatTheStackImageBakesIn(t *testing.T) {
	p, err := schema.LoadPack(".")
	if err != nil {
		t.Fatal(err)
	}
	for name, argv := range p.Checks {
		if len(argv) != 1 || !strings.HasPrefix(argv[0], "/opt/ygg/checks/") {
			t.Errorf("check %s is %v; it must be one script in /opt/ygg/checks, which the agent cannot edit", name, argv)
			continue
		}
		st, err := os.Stat(filepath.Join("devcontainer", "checks", filepath.Base(argv[0])))
		if err != nil || st.Mode()&0o111 == 0 {
			t.Errorf("check %s names %s, which devcontainer/checks does not hold as an executable: %v", name, argv[0], err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join("devcontainer", "checks"))
	for _, e := range entries {
		// docs.sh and sqlc-owned.sh are run by the checks, config/ is read by go-static.sh and go-deps.sh, and lib/ holds
		// the helper go-deps.sh runs
		used := e.Name() == "docs.sh" || e.Name() == "sqlc-owned.sh" || e.Name() == "config" || e.Name() == "lib"
		for _, argv := range p.Checks {
			used = used || filepath.Base(argv[0]) == e.Name()
		}
		if !used {
			t.Errorf("devcontainer/checks/%s is baked into the image and no check runs it", e.Name())
		}
	}
}

func TestTheServicesAreWhatAnAPIAsksForAndPinnedByDigest(t *testing.T) {
	p, err := schema.LoadPack(".")
	if err != nil {
		t.Fatal(err)
	}
	svc := map[string]gungnir.Service{}
	for _, s := range p.ServiceList() {
		svc[s.Name] = s
	}
	pg, s3 := svc["postgres"], svc["seaweedfs"]
	if len(svc) != 2 || pg.Ports[0] != 5432 || s3.Ports[0] != 8333 {
		t.Fatalf("services = %+v; an API gets Postgres on 5432 and SeaweedFS's S3 on 8333", svc)
	}
	if pg.Env["POSTGRES_DB"] != "app" || pg.Env["POSTGRES_HOST_AUTH_METHOD"] != "trust" {
		t.Errorf("postgres env = %v; go-integration.sh connects to database app as postgres, with no password", pg.Env)
	}
	if len(s3.Command) == 0 || s3.Command[0] != "server" {
		t.Errorf("seaweedfs command = %v; the image does nothing without one", s3.Command)
	}
	if err := gungnir.ValidateServices(p.ServiceList()); err != nil {
		t.Error(err)
	}
}

func TestTheImplementerPromptTellsWhatTheFactoryChecksAndWhatNotToDo(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("prompts", "implementer.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	scripts := map[string]string{}
	for _, f := range []string{"go-static.sh", "go-unit.sh", "go-contract.sh"} {
		s, _ := os.ReadFile(filepath.Join("devcontainer", "checks", f))
		scripts[f] = string(s)
	}
	// What the prompt tells the implementer to run is what the ladder runs.
	for cmd, script := range map[string]string{"gofmt -l .": "go-static.sh", "go vet ./...": "go-static.sh", "golangci-lint run": "go-static.sh",
		"go test -race ./...": "go-unit.sh", "sqlc diff": "go-contract.sh"} {
		if !strings.Contains(text, cmd) || !strings.Contains(scripts[script], strings.Fields(cmd)[0]) {
			t.Errorf("the prompt and %s disagree about %q", script, cmd)
		}
	}
	for _, want := range []string{"AGENTS.md", "Test first", "Do not run `git commit`", "openapi.yaml", "sqlc generate", ".devcontainer/", "Findings to fix"} {
		if !strings.Contains(text, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
}

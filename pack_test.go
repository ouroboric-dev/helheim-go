package helheim

import (
	"io/fs"
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
	want := []string{"static", "unit", "tdd", "contract", "deps", "security", "authz", "integration"}
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
		// docs.sh and sqlc-owned.sh are run by the checks, config/ is read by go-static.sh and go-deps.sh, protected-common is the list go-static.sh reads, and lib/ holds
		// the helper go-deps.sh runs
		used := e.Name() == "docs.sh" || e.Name() == "sqlc-owned.sh" || e.Name() == "config" || e.Name() == "lib" || e.Name() == "protected-common"
		for _, argv := range p.Checks {
			used = used || filepath.Base(argv[0]) == e.Name()
		}
		if !used {
			t.Errorf("devcontainer/checks/%s is baked into the image and no check runs it", e.Name())
		}
	}
}

// The services an API runs on are the runtime realm's (Niflheim's), asked for by need; the pack names none.
func TestThePackNeedsADatabaseAndAStore(t *testing.T) {
	p, err := schema.LoadPack(".")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Needs, ","); got != "postgres,s3" {
		t.Errorf("needs = %q, want postgres,s3", got)
	}
	for _, n := range p.Needs {
		if !gungnir.ValidNeed(n) {
			t.Errorf("need %q is not one of gungnir.Needs", n)
		}
	}
	if len(p.Services) != 0 {
		t.Errorf("services = %+v; the runtime realm provides them (RR-27)", p.Services)
	}
}

// walk calls fn with the path and text of every file in the realm except .git, LICENSES and go.sum.
func walk(t *testing.T, fn func(path, text string)) {
	t.Helper()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "LICENSES" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.sum" || !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNoPostgres17(t *testing.T) {
	re := regexp.MustCompile(`postgres:17|postgresql-17|postgresql/17|Postgres 17`)
	walk(t, func(path, text string) {
		if path == "pack_test.go" {
			return
		}
		if m := re.FindString(text); m != "" {
			t.Errorf("%s names %q; the runtime realm runs Postgres 18", path, m)
		}
	})
}

func TestTheStoreIsTheS3Service(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("devcontainer", "checks", "go-integration.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `YGG_S3_ENDPOINT="${YGG_S3_ENDPOINT:-http://s3:8333}"`) {
		t.Error("go-integration.sh's default YGG_S3_ENDPOINT is not http://s3:8333")
	}
	walk(t, func(path, text string) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		if strings.Contains(text, "seaweedfs:8333") || strings.Contains(text, "http://seaweedfs") {
			t.Errorf("%s names the store by seaweedfs; the runtime realm's service is s3", path)
		}
	})
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

// helheim-go has no graph in v0: the lead realm's graph holds the pointer-contract gate and the move that
// ships the routing indexes (midgard-react). What this realm owes routing is a contract Odin can find,
// and an oasdiff in its image for the gate. Odin finds a contract by a file name that ends in openapi.yaml.
func TestTheTemplatesContractIsNamedSoRoutingFindsIt(t *testing.T) {
	var found []string
	err := filepath.WalkDir("templates/api", func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(d.Name(), "openapi") {
			found = append(found, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("the api template holds no OpenAPI file")
	}
	for _, f := range found {
		if !strings.HasSuffix(f, "openapi.yaml") {
			t.Errorf("%s: routing finds a contract by a name that ends in openapi.yaml", f)
		}
	}
	script, err := os.ReadFile("devcontainer/checks/go-contract.sh")
	if err != nil || !strings.Contains(string(script), "oasdiff breaking") {
		t.Errorf("go-contract.sh must run oasdiff breaking, the evidence the pointer-contract gate accepts beside extracted edges: %v", err)
	}
}

func TestTheRealmHoldsNoGraph(t *testing.T) {
	if _, err := os.Stat("graph.yaml"); !os.IsNotExist(err) {
		t.Errorf("graph.yaml: stat = %v; a run takes its graph from the graph pack it names, not from this realm", err)
	}
	p, err := schema.LoadPack(".")
	if err != nil {
		t.Fatalf("LoadPack() = %v", err)
	}
	if p.Graph != "" {
		t.Errorf("graph = %q; this realm holds no graph", p.Graph)
	}
}

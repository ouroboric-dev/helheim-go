package helheim

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ouroboric-dev/gungnir/schema"
)

// first returns the first match of the pattern in text, or fails the test.
func first(t *testing.T, text, pattern, what string) []string {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("%s: no match for %s", what, pattern)
	}
	return m
}

// The product's first commit carries its own CI. It runs the stack's own commands on the stack's own
// images, so every value that belongs to the stack is read from the stack here, not written down twice.
func TestTemplateCI(t *testing.T) {
	read := func(p ...string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(p...))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	wf := read("templates", "api", ".github", "workflows", "ci.yml")
	cf := read("devcontainer", "Containerfile")

	// one job, named test
	jobs := first(t, wf, `(?m)^jobs:\n((?:[ ]+.*\n|\n)*)`, "the jobs")[1]
	if names := regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):\s*$`).FindAllStringSubmatch(jobs, -1); len(names) != 1 || names[0][1] != "test" {
		t.Errorf("jobs = %v; the workflow has the one job, test", names)
	}
	if !strings.Contains(wf, "\n  test:\n") {
		t.Error("no line `  test:` under jobs:")
	}

	// every action is pinned by commit SHA, with its version in a comment
	uses := regexp.MustCompile(`(?m)^\s*(?:- )?uses:.*$`).FindAllString(wf, -1)
	if len(uses) == 0 {
		t.Error("the workflow uses no action; it should check out the code")
	}
	pinned := regexp.MustCompile(`@[0-9a-f]{40}\s+# v`)
	for _, u := range uses {
		if !pinned.MatchString(u) {
			t.Errorf("%q is not pinned by a full commit SHA with its version in a comment", strings.TrimSpace(u))
		}
	}

	// the job runs in the image of the stack stage
	stack := first(t, cf, `(?m)^FROM (\S+) AS stack\s*$`, "the stack stage of devcontainer/Containerfile")[1]
	if got := first(t, wf, `(?m)^    container:\n      image: (?:>-\n        )?(\S+)\s*$`, "the job container")[1]; got != stack {
		t.Errorf("container image = %s, want the stack's %s", got, stack)
	}

	// Postgres is the pack's service, with its env, and the job waits for it
	p, err := schema.LoadPack(".")
	if err != nil {
		t.Fatal(err)
	}
	var pg string
	for _, s := range p.ServiceList() {
		if s.Name == "postgres" {
			pg = s.Image
		}
	}
	if pg == "" {
		t.Fatal("pack.yaml has no postgres service")
	}
	svc := first(t, wf, `(?m)^    services:\n((?:      .*\n)+)`, "the services")[1]
	if got := first(t, svc, `(?m)^      postgres:\n        image: (?:>-\n          )?(\S+)\s*$`, "the postgres service")[1]; got != pg {
		t.Errorf("postgres image = %s, want pack.yaml's %s", got, pg)
	}
	for _, want := range []string{"POSTGRES_DB: app", "POSTGRES_HOST_AUTH_METHOD: trust", "pg_isready"} {
		if !strings.Contains(svc, want) {
			t.Errorf("the postgres service lacks %q", want)
		}
	}
	if strings.Contains(svc, "seaweedfs") {
		t.Error("the workflow starts SeaweedFS; a service container cannot take its command")
	}

	// the least privilege, and the database the integration step reaches by the service's host name
	for _, want := range []string{
		"contents: read",
		"persist-credentials: false",
		"YGG_DATABASE_URL: postgres://postgres@postgres:5432/app?sslmode=disable",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the workflow lacks %q", want)
		}
	}

	// golangci-lint comes from the release the Containerfile checks, by the same hash
	m := first(t, cf, `fetch (https://github\.com/golangci/golangci-lint/releases/download/\S+) \S+ ([0-9a-f]{64})`, "the golangci-lint fetch in devcontainer/Containerfile")
	if !strings.Contains(wf, m[1]) || !strings.Contains(wf, m[2]) {
		t.Errorf("the workflow does not fetch golangci-lint from %s with SHA-256 %s", m[1], m[2])
	}
}

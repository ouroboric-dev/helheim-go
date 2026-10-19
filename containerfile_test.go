package helheim

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ouroboric-dev/gungnir/schema"
)

var (
	fromLine   = regexp.MustCompile(`(?m)^FROM\s+(\S+)`)
	digestRef  = regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]*(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}$`)
	fetchCall  = regexp.MustCompile(`fetch (https://\S+) (\S+) ([0-9a-f]{64})`)
	goInstall  = regexp.MustCompile(`go install (\S+)`)
	pipedShell = regexp.MustCompile(`\|\s*(sudo\s+)?(ba|z)?sh\b`)
)

func TestTheStackImageIsMadeOfPinnedInputsOnly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("devcontainer", "Containerfile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	froms := fromLine.FindAllStringSubmatch(text, -1)
	if len(froms) == 0 {
		t.Fatal("the Containerfile has no FROM line")
	}
	for _, f := range froms {
		if !digestRef.MatchString(f[1]) {
			t.Errorf("FROM %s is not pinned by digest", f[1])
		}
	}
	fetches := fetchCall.FindAllStringSubmatch(text, -1)
	if len(fetches) < 5 {
		t.Errorf("%d downloads are checked against a SHA-256; the ladder's five release binaries are", len(fetches))
	}
	if n := strings.Count(text, "curl "); n != 1 {
		t.Errorf("curl appears %d times; the one use is inside fetch, which checks the hash", n)
	}
	if !strings.Contains(text, `echo "$3  $2" | sha256sum -c -`) {
		t.Error("fetch does not check the download against its SHA-256 with sha256sum -c")
	}
	for _, f := range fetches {
		if !regexp.MustCompile(`/v?\d+\.\d+\.\d+/`).MatchString(f[1]) {
			t.Errorf("the download %s names no version", f[1])
		}
	}
	installs := goInstall.FindAllStringSubmatch(text, -1)
	if len(installs) == 0 {
		t.Error("govulncheck is installed with go install, at a version")
	}
	for _, i := range installs {
		if !regexp.MustCompile(`@v\d+\.\d+\.\d+$`).MatchString(i[1]) {
			t.Errorf("go install %s is not at a version", i[1])
		}
	}
	if pipedShell.MatchString(text) || strings.Contains(text, "@latest") || strings.Contains(strings.ToLower(text), ":latest") {
		t.Error("the Containerfile pipes a download into a shell or names a floating version")
	}
	// A root-owned /go/pkg would stop the agent's user from making a module cache (found on a real engine).
	if !strings.Contains(text, "rm -rf /go/pkg") {
		t.Error("the Containerfile leaves /go/pkg to root; the agent's user could not make its module cache")
	}
	// /go/bin is on PATH ahead of the tools and the golang image makes it world-writable: the agent could
	// shadow go or golangci-lint there (final review C-1).
	if !strings.Contains(text, "chown root:root /go/bin") || !strings.Contains(text, "chmod 0755 /go/bin") {
		t.Error("the Containerfile leaves /go/bin writable by the agent's user, and it is on PATH ahead of the tools")
	}
	for _, want := range []string{"golangci-lint", "sqlc", "oasdiff", "vacuum", "osv-scanner", "govulncheck", "COPY checks/ /opt/ygg/checks/", "go-w"} {
		if !strings.Contains(text, want) {
			t.Errorf("the Containerfile lacks %q", want)
		}
	}
	// Every stage the build context can reach is a file of the realm: no link leads out of it, and it is
	// small enough to hash into a tag (gungnir's rule for a build context).
	if _, err := schema.HashContext("devcontainer"); err != nil {
		t.Errorf("devcontainer is not a build context: %v", err)
	}
}

func TestEveryCheckScriptIsExecutableAndShellcheckedByItsAuthor(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("devcontainer", "checks"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "protected-common" { // the list of protected files is data, not a script
			continue
		}
		p := filepath.Join("devcontainer", "checks", e.Name())
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable", p)
		}
		b, _ := os.ReadFile(p)
		if !strings.HasPrefix(string(b), "#!/bin/bash\n") || !strings.Contains(string(b), "set -euo pipefail") {
			t.Errorf("%s does not start with #!/bin/bash and set -euo pipefail", p)
		}
	}
}

// The lint config is baked into /opt/ygg/checks/config with the scripts, where the agent cannot write: the
// COPY takes the whole folder, and the scripts read the config from beside themselves (B-049).
func TestTheLintConfigIsBakedWithTheScripts(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("devcontainer", "Containerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "COPY checks/ /opt/ygg/checks/") || !strings.Contains(string(b), "chmod -R a+rX,go-w /opt/ygg/checks") {
		t.Error("the Containerfile does not copy the whole checks folder as read-only")
	}
	if _, err := os.Stat(filepath.Join("devcontainer", "checks", "config", "golangci.yml")); err != nil {
		t.Error(err)
	}
	s, _ := os.ReadFile(filepath.Join("devcontainer", "checks", "go-static.sh"))
	if !strings.Contains(string(s), `$(dirname "$0")/config/golangci.yml`) {
		t.Error("go-static.sh does not read the config from beside itself")
	}
}

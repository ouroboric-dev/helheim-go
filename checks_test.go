package helheim

import (
	"archive/zip"
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The scripts of devcontainer/checks are baked into the stack image and run in a leaf. These tests run them
// on small leaves made in a temp dir, with the real go, gofmt and git, and stand-ins for the tools that only
// the image has (golangci-lint, sqlc, oasdiff, vacuum, govulncheck, osv-scanner), which log how they were
// called and exit as the test says.

// leaf writes files (path: body) into a new temp dir and returns it.
func leaf(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	put(t, dir, files)
	return dir
}

func put(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// stubs makes a folder of executables named tools. Each logs "<tool> <args>" to calls.log in the folder and
// exits with the number in the variable STUB_<TOOL>_EXIT (0 when it is unset), after printing STUB_<TOOL>_OUT.
func stubs(t *testing.T, tools ...string) (bin string) {
	t.Helper()
	bin = t.TempDir()
	for _, tool := range tools {
		v := strings.ToUpper(strings.ReplaceAll(tool, "-", "_"))
		body := "#!/bin/sh\necho \"" + tool + " $*\" >> \"$(dirname \"$0\")/calls.log\"\n" +
			"[ -z \"$STUB_" + v + "_OUT\" ] || echo \"$STUB_" + v + "_OUT\"\nexit \"${STUB_" + v + "_EXIT:-0}\"\n"
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

func calls(bin string) string {
	b, _ := os.ReadFile(filepath.Join(bin, "calls.log"))
	return string(b)
}

// script runs devcontainer/checks/name in dir with bin first on PATH and the given extra environment
// (KEY=value), and returns its exit code and its output.
func script(t *testing.T, dir, bin, name string, env ...string) (int, string) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("devcontainer", "checks", name))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", abs)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	path := os.Getenv("PATH")
	if bin != "" {
		path = bin + ":" + path
		cmd.Env = append(cmd.Env, "PATH="+path)
	}
	// The scripts fix their own PATH (C-1); a test says what the image's fixed PATH would be here.
	cmd.Env = append(cmd.Env, "YGG_CHECKS_PATH="+path)
	cmd.Env = append(cmd.Env, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String()
	}
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return 0, out.String()
}

const goMod = "module t\n\ngo 1.26\n"

func TestDocsAcceptsDocumentsAndNamesEveryProblemOfTheOthers(t *testing.T) {
	good := "---\ntitle: A\nstatus: draft\ndate: 2026-10-17\n---\n# A\n\nSee [b](b.md), [a part](b.md#part), [the web](https://example.com/x) and [here](#top).\n"
	dir := leaf(t, map[string]string{"docs/a.md": good, "docs/b.md": good})
	if code, out := script(t, dir, "", "docs.sh"); code != 0 || !strings.Contains(out, "docs: ok") {
		t.Errorf("good documents: exit %d\n%s", code, out)
	}
	if code, out := script(t, t.TempDir(), "", "docs.sh"); code != 0 || !strings.Contains(out, "nothing to check") {
		t.Errorf("a leaf with no docs/: exit %d\n%s", code, out)
	}
	dir = leaf(t, map[string]string{
		"docs/b.md":            good,
		"docs/NoPlain Name.md": good,
		"docs/no-front.md":     "# no front matter\n",
		"docs/no-status.md":    "---\ntitle: A\ndate: 2026-10-17\n---\ntext\n",
		"docs/odd-status.md":   "---\ntitle: A\nstatus: maybe\ndate: 17 October\n---\ntext\n",
		"docs/dead-link.md":    strings.Replace(good, "[b](b.md)", "[gone](gone.md)", 1),
	})
	code, out := script(t, dir, "", "docs.sh")
	if code == 0 {
		t.Fatalf("bad documents passed:\n%s", out)
	}
	for _, want := range []string{"NoPlain Name.md: the file name must be", "no-front.md: starts without front matter", "no-status.md: the front matter has no status",
		"dead-link.md: the link gone.md points at nothing", "odd-status.md: status is maybe", "odd-status.md: date is 17 October", "6 problem(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

func TestStaticStopsAtTheFirstProblemAndSaysWhatToFix(t *testing.T) {
	bin := stubs(t, "golangci-lint")
	clean := map[string]string{"go.mod": goMod, "a.go": "package t\n\nfunc A() int { return 1 }\n"}
	if code, out := script(t, leaf(t, clean), bin, "go-static.sh"); code != 0 || !strings.Contains(calls(bin), "golangci-lint run ./...") {
		t.Fatalf("a clean leaf: exit %d\n%s\ncalls: %s", code, out, calls(bin))
	}
	bin = stubs(t, "golangci-lint")
	unformatted := map[string]string{"go.mod": goMod, "a.go": "package t\nfunc A() int {return 1}\n"}
	code, out := script(t, leaf(t, unformatted), bin, "go-static.sh")
	if code == 0 || !strings.Contains(out, "not gofmt-clean") || !strings.Contains(out, "a.go") || calls(bin) != "" {
		t.Errorf("an unformatted file: exit %d, calls %q\n%s", code, calls(bin), out)
	}
	vet := map[string]string{"go.mod": goMod, "a.go": "package t\n\nimport \"fmt\"\n\nfunc A() { fmt.Printf(\"%d\", \"x\") }\n"}
	if code, out := script(t, leaf(t, vet), stubs(t, "golangci-lint"), "go-static.sh"); code == 0 || !strings.Contains(out, "go vet") {
		t.Errorf("a vet problem: exit %d\n%s", code, out)
	}
	tagged := map[string]string{"go.mod": goMod, "a.go": "package t\n", "a_it_test.go": "//go:build integration\n\npackage t\n\nimport \"fmt\"\n\nfunc init() { fmt.Printf(\"%d\", \"x\") }\n"}
	if code, _ := script(t, leaf(t, tagged), stubs(t, "golangci-lint"), "go-static.sh"); code == 0 {
		t.Error("a vet problem in a file behind the integration tag passed; vet must look there too")
	}
	bin = stubs(t, "golangci-lint")
	if code, _ := script(t, leaf(t, clean), bin, "go-static.sh", "STUB_GOLANGCI_LINT_EXIT=1", "STUB_GOLANGCI_LINT_OUT=a.go:3: unused"); code == 0 {
		t.Error("a lint failure passed")
	}
	docs := map[string]string{"go.mod": goMod, "a.go": "package t\n", "docs/Bad Name.md": "x"}
	if code, out := script(t, leaf(t, docs), stubs(t, "golangci-lint"), "go-static.sh"); code == 0 || !strings.Contains(out, "docs:") {
		t.Errorf("a bad document: exit %d\n%s", code, out)
	}
}

func TestUnitRunsEveryTestWithTheRaceDetectorAndNoCache(t *testing.T) {
	pass := map[string]string{"go.mod": goMod, "a.go": "package t\n", "a_test.go": "package t\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"}
	if code, out := script(t, leaf(t, pass), "", "go-unit.sh"); code != 0 {
		t.Errorf("a passing leaf: exit %d\n%s", code, out)
	}
	fail := map[string]string{"go.mod": goMod, "a.go": "package t\n", "a_test.go": "package t\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Fatal(\"broken\") }\n"}
	if code, out := script(t, leaf(t, fail), "", "go-unit.sh"); code == 0 || !strings.Contains(out, "broken") {
		t.Errorf("a failing test: exit %d\n%s", code, out)
	}
	racy := map[string]string{"go.mod": goMod, "a.go": "package t\n", "a_test.go": "package t\n\nimport \"testing\"\n\nfunc TestRace(t *testing.T) {\n\tn, done := 0, make(chan bool)\n\tgo func() {\n\t\tfor i := 0; i < 1000; i++ {\n\t\t\tn++\n\t\t}\n\t\tdone <- true\n\t}()\n\tfor i := 0; i < 1000; i++ {\n\t\tn++\n\t}\n\t<-done\n}\n"}
	if code, out := script(t, leaf(t, racy), "", "go-unit.sh"); code == 0 || !strings.Contains(out, "DATA RACE") {
		t.Errorf("a test with a data race: exit %d\n%s", code, out)
	}
}

// repo makes a git repo with the given files committed on main, and returns it.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := leaf(t, files)
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "start")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.invalid"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// work puts files on a branch of the repo made from main, committed.
func work(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	git(t, dir, "switch", "-q", "-c", "work")
	put(t, dir, files)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "work")
}

const (
	aGo     = "package t\n\nfunc A() int { return 1 }\n"
	aTestGo = "package t\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"A\")\n\t}\n}\n"
)

func TestTDDWantsTheNewTestsToFailOnTheCommitTheTaskStartedFrom(t *testing.T) {
	start := map[string]string{"go.mod": goMod, "a.go": aGo, "a_test.go": aTestGo}
	cases := []struct {
		name  string
		files map[string]string
		pass  bool
		want  string
	}{
		{"code and a test that needs it", map[string]string{"b.go": "package t\n\nfunc B() int { return 2 }\n",
			"b_test.go": "package t\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {\n\tif B() != 2 {\n\t\tt.Fatal(\"B\")\n\t}\n}\n"}, true, "fail on the commit the task started from"},
		{"code with no test", map[string]string{"b.go": "package t\n\nfunc B() int { return 2 }\n"}, false, "changed without a new or changed test"},
		{"a test that passes before the code", map[string]string{"c_test.go": "package t\n\nimport \"testing\"\n\nfunc TestAgain(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"A\")\n\t}\n}\n"}, false, "test nothing new"},
		{"a changed test that fails before the code", map[string]string{"a.go": "package t\n\nfunc A() int { return 3 }\n",
			"a_test.go": strings.Replace(aTestGo, "A() != 1", "A() != 3", 1)}, true, "fail on the commit the task started from"},
		{"a file that says it is generated and no generator owns", map[string]string{"gen.go": "// Code generated by hand. DO NOT EDIT.\n\npackage t\n\nvar G = 1\n"}, false, "gen.go"},
		{"only a document", map[string]string{"README.md": "x\n"}, true, "no Go code changed"},
		{"only a test behind the integration tag", map[string]string{"db_test.go": "//go:build integration\n\npackage t\n\nimport \"testing\"\n\nfunc TestDB(t *testing.T) {}\n"}, true, "cannot be shown to fail"},
	}
	for _, c := range cases {
		dir := repo(t, start)
		work(t, dir, c.files)
		code, out := script(t, dir, "", "go-tdd.sh")
		if (code == 0) != c.pass || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want pass=%v and %q\n%s", c.name, code, c.pass, c.want, out)
		}
	}
	// Nothing changed on the branch: nothing to check.
	dir := repo(t, start)
	git(t, dir, "switch", "-q", "-c", "work")
	if code, out := script(t, dir, "", "go-tdd.sh"); code != 0 || !strings.Contains(out, "no Go code changed") {
		t.Errorf("an unchanged branch: exit %d\n%s", code, out)
	}
	// The leaf's own tree is left as it was: the check works in a folder of its own.
	dir = repo(t, start)
	work(t, dir, map[string]string{"c_test.go": "package t\n\nimport \"testing\"\n\nfunc TestAgain(t *testing.T) {}\n"})
	script(t, dir, "", "go-tdd.sh")
	if st := git(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("the check left the leaf changed:\n%s", st)
	}
}

func TestContractLintsTheContractChecksGeneratedCodeAndListsBreakingChanges(t *testing.T) {
	start := map[string]string{"openapi.yaml": "openapi: 3.1.0\n", "sqlc.yaml": "version: \"2\"\n"}
	dir := repo(t, start)
	work(t, dir, map[string]string{"openapi.yaml": "openapi: 3.1.0\ninfo: {}\n"})
	bin := stubs(t, "vacuum", "sqlc", "oasdiff")
	code, out := script(t, dir, bin, "go-contract.sh", `STUB_OASDIFF_OUT=[{"id":"response-success-status-removed","text":"DELETE /x removed"}]`)
	got := calls(bin)
	for _, want := range []string{"vacuum lint --no-style --details --fail-severity error openapi.yaml", "sqlc diff", "oasdiff breaking "} {
		if !strings.Contains(got, want) {
			t.Errorf("the tools were called %q, want %q", got, want)
		}
	}
	if code != 0 || !strings.Contains(out, "BREAKING") || !strings.Contains(out, "DELETE /x removed") || !strings.Contains(got, "oasdiff breaking") {
		t.Errorf("a breaking change is listed and not refused: exit %d\n%s", code, out)
	}
	bin = stubs(t, "vacuum", "sqlc", "oasdiff")
	if code, out := script(t, dir, bin, "go-contract.sh", "STUB_OASDIFF_OUT=[]"); code != 0 || !strings.Contains(out, "none") || strings.Contains(out, "BREAKING") {
		t.Errorf("no breaking change: exit %d\n%s", code, out)
	}
	for name, env := range map[string]string{"a lint error": "STUB_VACUUM_EXIT=1", "generated code that is out of date": "STUB_SQLC_EXIT=1"} {
		if code, out := script(t, dir, stubs(t, "vacuum", "sqlc", "oasdiff"), "go-contract.sh", env); code == 0 {
			t.Errorf("%s passed:\n%s", name, out)
		}
	}
	if code, out := script(t, repo(t, map[string]string{"a.txt": "x"}), stubs(t, "vacuum", "sqlc", "oasdiff"), "go-contract.sh"); code == 0 || !strings.Contains(out, "no openapi.yaml") {
		t.Errorf("a leaf with no contract: exit %d\n%s", code, out)
	}
	// A contract that did not exist when the task started is new, with nothing to break.
	fresh := repo(t, map[string]string{"sqlc.yaml": "x"})
	work(t, fresh, map[string]string{"openapi.yaml": "openapi: 3.1.0\n"})
	bin = stubs(t, "vacuum", "sqlc", "oasdiff")
	if code, out := script(t, fresh, bin, "go-contract.sh"); code != 0 || !strings.Contains(out, "is new in this change") || strings.Contains(calls(bin), "oasdiff") {
		t.Errorf("a new contract: exit %d, calls %q\n%s", code, calls(bin), out)
	}
}

func TestSecurityStopsOnAVulnerabilityFromEitherSource(t *testing.T) {
	dir := leaf(t, map[string]string{"go.mod": goMod})
	bin := stubs(t, "govulncheck", "osv-scanner")
	if code, out := script(t, dir, bin, "go-security.sh"); code != 0 {
		t.Fatalf("a clean leaf: exit %d\n%s", code, out)
	}
	for _, want := range []string{"govulncheck ./...", "govulncheck -tags integration ./...", "osv-scanner scan source -L go.mod"} {
		if !strings.Contains(calls(bin), want) {
			t.Errorf("calls %q lack %q", calls(bin), want)
		}
	}
	for _, env := range []string{"STUB_GOVULNCHECK_EXIT=3", "STUB_OSV_SCANNER_EXIT=1"} {
		if code, _ := script(t, dir, stubs(t, "govulncheck", "osv-scanner"), "go-security.sh", env); code == 0 {
			t.Errorf("%s passed", env)
		}
	}
}

func TestIntegrationWaitsForPostgresAndRunsTheTaggedTestsWithItsAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	files := map[string]string{"go.mod": goMod, "a.go": "package t\n", "a_it_test.go": "//go:build integration\n\npackage t\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
		"func TestURL(t *testing.T) {\n\tif os.Getenv(\"YGG_DATABASE_URL\") != \"postgres://postgres@127.0.0.1:" + strconv.Itoa(port) + "/app?sslmode=disable\" {\n\t\tt.Fatal(os.Getenv(\"YGG_DATABASE_URL\"))\n\t}\n}\n"}
	env := []string{"YGG_DB_HOST=127.0.0.1", "YGG_DB_PORT=" + strconv.Itoa(port)}
	if code, out := script(t, leaf(t, files), "", "go-integration.sh", env...); code != 0 {
		t.Errorf("with a database: exit %d\n%s", code, out)
	}
	ln.Close()
	if code, out := script(t, leaf(t, files), "", "go-integration.sh", append(env, "YGG_DB_WAIT=2")...); code == 0 || !strings.Contains(out, "did not answer") {
		t.Errorf("with no database: exit %d\n%s", code, out)
	}
}

// The tools the ladder calls are found on a PATH the scripts fix themselves, not on the one they are
// started with, which may hold a folder the agent can write (final review C-1).
func TestTheScriptsFixTheirOwnPathAndIgnoreAnAgentsFolderOnIt(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("devcontainer", "checks"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join("devcontainer", "checks", e.Name()))
		if !strings.Contains(string(b), `PATH="${YGG_CHECKS_PATH:-/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin}"`) {
			t.Errorf("%s does not fix its PATH to the image's root-owned folders", e.Name())
		}
	}
	evil := t.TempDir()
	if err := os.WriteFile(filepath.Join(evil, "go"), []byte("#!/bin/sh\necho FAKE-GO\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(filepath.Join("devcontainer", "checks", "go-unit.sh"))
	cmd := exec.Command("bash", abs)
	cmd.Dir = leaf(t, map[string]string{"go.mod": goMod})
	cmd.Env = append(os.Environ(), "PATH="+evil+":"+os.Getenv("PATH"))
	cmd.Env = slicesWithout(cmd.Env, "YGG_CHECKS_PATH=")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "FAKE-GO") {
		t.Errorf("go-unit.sh ran a go found in a folder the caller put first on PATH:\n%s", out)
	}
}

func slicesWithout(env []string, prefix string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// A moved file with new code in it is still new code (final review I-3): git's rename detection must not
// hide it from the check.
func TestTDDSeesACodeFileThatWasMovedAndChanged(t *testing.T) {
	start := map[string]string{"go.mod": goMod, "a.go": aGo + "\n// pad pad pad pad pad pad pad pad\n// pad pad pad pad pad pad pad pad\n// pad pad pad pad pad pad pad pad\n// pad pad pad pad pad pad pad pad\n", "a_test.go": aTestGo}
	dir := repo(t, start)
	git(t, dir, "switch", "-q", "-c", "work")
	git(t, dir, "mv", "a.go", "routes.go")
	body := start["a.go"] + "\nfunc B() int { return 2 }\n"
	put(t, dir, map[string]string{"routes.go": body})
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "move")
	if st := git(t, dir, "diff", "--name-status", "main", "HEAD"); !strings.HasPrefix(st, "R") {
		t.Fatalf("the setup is not a rename:\n%s", st)
	}
	code, out := script(t, dir, "", "go-tdd.sh")
	if code == 0 || !strings.Contains(out, "changed without a new or changed test") || !strings.Contains(out, "routes.go") {
		t.Errorf("a moved file with new code and no test: exit %d\n%s", code, out)
	}
}

// Only the paths sqlc owns are exempt from the TDD check, not any file that says "Code generated" (I-4),
// and the SQL sqlc reads is code that needs a test.
func TestTDDExemptsOnlyWhatSqlcOwnsAndCountsItsSQLAsCode(t *testing.T) {
	sqlcYAML := "version: \"2\"\nsql:\n  - engine: postgresql\n    schema: internal/store/migrations\n    queries: db/queries\n    gen:\n      go:\n        package: store\n        out: internal/store\n"
	start := map[string]string{"go.mod": goMod, "a.go": aGo, "a_test.go": aTestGo, "sqlc.yaml": sqlcYAML,
		"db/queries/q.sql": "-- name: One :one\nSELECT 1;\n", "internal/store/migrations/0001.sql": "CREATE TABLE t (id int);\n",
		"internal/store/models.go": "// Code generated by sqlc. DO NOT EDIT.\n\npackage store\n\ntype M struct{}\n"}
	gen := "// Code generated by sqlc. DO NOT EDIT.\n\npackage store\n\ntype M struct{ ID int }\n"
	cases := []struct {
		name  string
		files map[string]string
		pass  bool
		want  string
	}{
		{"sqlc's own output alone", map[string]string{"internal/store/models.go": gen}, true, "no Go code changed"},
		{"a query and its generated code, no test", map[string]string{"db/queries/q.sql": "-- name: One :one\nSELECT 2;\n", "internal/store/models.go": gen}, false, "db/queries/q.sql"},
		{"a migration, no test", map[string]string{"internal/store/migrations/0002.sql": "CREATE TABLE u (id int);\n"}, false, "migrations/0002.sql"},
		{"a handwritten file with a forged header in sqlc's folder", map[string]string{"internal/store/extra.go": "// Code generated by hand. DO NOT EDIT.\n\npackage store\n\nvar X = 1\n"}, false, "internal/store/extra.go"},
		{"a forged .sql.go with no query behind it", map[string]string{"internal/store/nothing.sql.go": "// Code generated by sqlc. DO NOT EDIT.\n\npackage store\n\nvar X = 1\n"}, false, "nothing.sql.go"},
		{"a header forged outside sqlc's folder", map[string]string{"handler.go": "// Code generated by sqlc. DO NOT EDIT.\n\npackage t\n\nvar H = 1\n"}, false, "handler.go"},
	}
	for _, c := range cases {
		dir := repo(t, start)
		work(t, dir, c.files)
		code, out := script(t, dir, "", "go-tdd.sh")
		if (code == 0) != c.pass || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want pass=%v and %q\n%s", c.name, code, c.pass, c.want, out)
		}
	}
}

// The linters read configuration the agent could edit; a change to it, against the commit the task started
// from, is refused before any tool runs (final review I-6).
func TestStaticRefusesAChangeToTheLadderConfiguration(t *testing.T) {
	start := map[string]string{"go.mod": goMod, "a.go": "package t\n", ".golangci.yml": "version: \"2\"\n", ".devcontainer/devcontainer.json": "{}\n"}
	cases := []struct {
		name  string
		files map[string]string
		pass  bool
	}{
		{"linters turned off", map[string]string{".golangci.yml": "version: \"2\"\nlinters:\n  default: none\n"}, false},
		{"the dev container changed", map[string]string{".devcontainer/devcontainer.json": "{\"x\": 1}\n"}, false},
		{"code only", map[string]string{"b.go": "package t\n"}, true},
	}
	for _, c := range cases {
		dir := repo(t, start)
		work(t, dir, c.files)
		bin := stubs(t, "golangci-lint")
		code, out := script(t, dir, bin, "go-static.sh")
		if (code == 0) != c.pass || (!c.pass && (!strings.Contains(out, "protected") || calls(bin) != "")) {
			t.Errorf("%s: exit %d, calls %q\n%s", c.name, code, calls(bin), out)
		}
	}
	// An uncommitted edit and a deletion count too.
	dir := repo(t, start)
	git(t, dir, "switch", "-q", "-c", "work")
	put(t, dir, map[string]string{".golangci.yml": "linters:\n  default: none\n"})
	if code, out := script(t, dir, stubs(t, "golangci-lint"), "go-static.sh"); code == 0 || !strings.Contains(out, ".golangci.yml") {
		t.Errorf("an uncommitted edit: exit %d\n%s", code, out)
	}
	dir = repo(t, start)
	git(t, dir, "switch", "-q", "-c", "work")
	git(t, dir, "rm", "-q", ".golangci.yml")
	git(t, dir, "commit", "-q", "-m", "drop")
	if code, out := script(t, dir, stubs(t, "golangci-lint"), "go-static.sh"); code == 0 || !strings.Contains(out, ".golangci.yml") {
		t.Errorf("a deleted configuration: exit %d\n%s", code, out)
	}
}

// proxyModule writes a one-package module into a file proxy (no network), and returns the proxy's URL.
func proxyModule(t *testing.T, path, version, src string) string {
	t.Helper()
	proxy := t.TempDir()
	dir := filepath.Join(proxy, filepath.FromSlash(path), "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module " + path + "\n\ngo 1.26\n"
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	for name, body := range map[string]string{"go.mod": mod, "dep.go": src} {
		w, err := zw.Create(path + "@" + version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		version + ".zip":  zbuf.Bytes(),
		version + ".mod":  []byte(mod),
		version + ".info": []byte(`{"Version":"` + version + `","Time":"2026-01-01T00:00:00Z"}`),
		"list":            []byte(version + "\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return "file://" + proxy
}

// The module cache is kept between runs and the agent can write it; a dependency edited there (an init that
// exits 0 makes every failing test report ok) must fail the rung (final review N-1).
func TestEveryGoCheckRefusesATamperedModuleCache(t *testing.T) {
	proxyURL := proxyModule(t, "example.invalid/dep", "v1.0.0", "package dep\n\nfunc D() int { return 1 }\n")
	cache := t.TempDir()
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", cache).Run() }) // the cache is read-only
	env := []string{"GOMODCACHE=" + cache, "GOPROXY=" + proxyURL, "GOSUMDB=off", "GONOSUMDB=*"}
	files := map[string]string{
		"go.mod":    "module t\n\ngo 1.26\n\nrequire example.invalid/dep v1.0.0\n",
		"a.go":      "package t\n\nimport \"example.invalid/dep\"\n\nfunc A() int { return dep.D() }\n",
		"a_test.go": "package t\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Fatal(\"broken\") }\n",
	}
	dir := repo(t, files)
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = append(os.Environ(), append(env, "GOWORK=off", "GOFLAGS=-mod=mod")...)
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	git(t, dir, "add", "go.sum")
	git(t, dir, "commit", "-q", "-m", "sum")
	git(t, dir, "switch", "-q", "-c", "work")
	put(t, dir, map[string]string{"b.go": "package t\n"})
	git(t, dir, "add", "b.go")
	git(t, dir, "commit", "-q", "-m", "work")
	names := []string{"go-static.sh", "go-unit.sh", "go-tdd.sh", "go-integration.sh", "go-security.sh"}
	run := func(name string) (int, string) {
		bin := stubs(t, "golangci-lint", "govulncheck", "osv-scanner")
		return script(t, dir, bin, name, append([]string{"YGG_DB_WAIT=0"}, env...)...)
	}
	// With the cache as it was downloaded, none of them stops at the cache.
	for _, name := range names {
		if _, out := run(name); strings.Contains(out, "module cache") {
			t.Errorf("%s refused an untouched cache:\n%s", name, out)
		}
	}
	dep := filepath.Join(cache, "example.invalid", "dep@v1.0.0")
	if err := exec.Command("chmod", "-R", "u+w", cache).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "evil.go"), []byte("package dep\n\nimport \"os\"\n\nfunc init() { os.Exit(0) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if code, out := run(name); code == 0 || !strings.Contains(out, "module cache") {
			t.Errorf("%s with a dependency edited in the cache: exit %d\n%s", name, code, out)
		}
	}
}

// golangci-lint skips a file with a "Code generated" header, and the agent can write one. With the template's
// configuration only the files sqlc writes are skipped (final review I-4).
func TestTemplateLintingSkipsOnlyWhatSqlcWrites(t *testing.T) {
	lint, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint is not installed here")
	}
	cfg, err := os.ReadFile(filepath.Join("templates", "api", ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// An unchecked error: errcheck reports it, unless the file is skipped.
	body := func(header string) string {
		return header + "package store\n\nimport \"os\"\n\nfunc Leak() { os.Remove(\"x\") }\n"
	}
	forged := "// Code generated by hand. DO NOT EDIT.\n\n"
	sqlc := "// Code generated by sqlc. DO NOT EDIT.\n\n"
	cases := []struct {
		name string
		file string
		src  string
		pass bool
	}{
		{"a handwritten file with a forged header", "internal/store/handler.go", body(forged), false},
		{"a forged header outside the store", "internal/api/handler.go", strings.Replace(body(forged), "package store", "package api", 1), false},
		{"a forged sqlc header on a file sqlc does not write", "internal/store/extra.go", body(sqlc), false},
		{"the models sqlc writes", "internal/store/models.go", body(sqlc), true},
		{"the queries sqlc writes", "internal/store/info.sql.go", body(sqlc), true},
	}
	for _, c := range cases {
		dir := repo(t, map[string]string{"go.mod": goMod, ".golangci.yml": string(cfg), c.file: c.src})
		code, out := script(t, dir, filepath.Dir(lint), "go-static.sh")
		if (code == 0) != c.pass {
			t.Errorf("%s: exit %d\n%s", c.name, code, out)
		}
	}
}

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
	if code, out := script(t, leaf(t, clean), bin, "go-static.sh"); code != 0 || !strings.Contains(calls(bin), "golangci-lint run --config ") || !strings.Contains(calls(bin), " ./...") {
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
		if e.IsDir() {
			continue
		}
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

// The tests below that need sqlc or golangci-lint run the real tools and skip when the machine lacks them
// (the image has both).
func needTools(t *testing.T, tools ...string) string {
	t.Helper()
	var dir string
	for _, tool := range tools {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s is not installed here", tool)
		}
		dir = filepath.Dir(p)
	}
	return dir
}

const sqlcYAML = "version: \"2\"\nsql:\n  - engine: postgresql\n    schema: internal/store/migrations\n    queries: db/queries\n    gen:\n      go:\n        package: store\n        out: internal/store\n"

// sqlcLeaf is a leaf with a sqlc.yaml, a migration and a query, and the code sqlc writes from them (db.go,
// models.go and info.sql.go), made by the real sqlc.
func sqlcLeaf(t *testing.T) map[string]string {
	t.Helper()
	needTools(t, "sqlc")
	src := map[string]string{"go.mod": goMod, "a.go": aGo, "a_test.go": aTestGo, "sqlc.yaml": sqlcYAML,
		"db/queries/info.sql":                "-- name: CountT :one\nSELECT count(*) FROM t;\n",
		"internal/store/migrations/0001.sql": "CREATE TABLE t (id int NOT NULL);\n"}
	dir := leaf(t, src)
	cmd := exec.Command("sqlc", "generate")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlc generate: %v\n%s", err, out)
	}
	for _, f := range []string{"db.go", "models.go", "info.sql.go"} {
		b, err := os.ReadFile(filepath.Join(dir, "internal", "store", f))
		if err != nil {
			t.Fatal(err)
		}
		src["internal/store/"+f] = string(b)
	}
	return src
}

const forgedHeader = "// Code generated by sqlc. DO NOT EDIT.\n\npackage store\n\nimport \"os\"\n\nfunc Leak() { os.Remove(\"x\") }\n"

// Only what sqlc generates, from the sqlc.yaml of the commit the task started from, and byte for byte, is
// exempt from the TDD check: not a file that says "Code generated", not a folder, and the SQL sqlc reads is
// code that needs a test (B-049, final review I-4).
func TestTDDExemptsOnlyWhatSqlcGeneratesAndCountsItsSQLAsCode(t *testing.T) {
	start := sqlcLeaf(t)
	stale := map[string]string{}
	for k, v := range start {
		stale[k] = v
	}
	stale["internal/store/models.go"] = "// Code generated by sqlc. DO NOT EDIT.\n\npackage store\n\ntype Old struct{}\n"
	cases := []struct {
		name  string
		start map[string]string
		files map[string]string
		pass  bool
		want  string
	}{
		{"sqlc's own output alone", stale, map[string]string{"internal/store/models.go": start["internal/store/models.go"]}, true, "no Go code changed"},
		{"a query, no test", start, map[string]string{"db/queries/info.sql": "-- name: CountT :one\nSELECT count(*) + 1 FROM t;\n"}, false, "db/queries/info.sql"},
		{"a migration, no test", start, map[string]string{"internal/store/migrations/0002.sql": "CREATE TABLE u (id int);\n"}, false, "migrations/0002.sql"},
		{"sqlc's file edited by hand, header kept", start, map[string]string{"internal/store/models.go": start["internal/store/models.go"] + "\nvar X = 1\n"}, false, "internal/store/models.go"},
		{"a handwritten file with a forged header in sqlc's folder", start, map[string]string{"internal/store/extra.go": forgedHeader}, false, "internal/store/extra.go"},
		{"a handwritten querier.go with a forged sqlc header", start, map[string]string{"internal/store/querier.go": forgedHeader}, false, "internal/store/querier.go"},
		{"a forged .sql.go with no query behind it", start, map[string]string{"internal/store/nothing.sql.go": forgedHeader}, false, "nothing.sql.go"},
		{"a header forged outside sqlc's folder", start, map[string]string{"handler.go": strings.Replace(forgedHeader, "package store", "package t", 1)}, false, "handler.go"},
		{"a forged querier.go and a sqlc.yaml that now says it writes one", start, map[string]string{"internal/store/querier.go": forgedHeader,
			"sqlc.yaml": sqlcYAML + "        emit_interface: true\n"}, false, "internal/store/querier.go"},
		{"a forged querier.go and a sqlc.yaml that is a link to another", start, nil, false, "internal/store/querier.go"},
	}
	for _, c := range cases {
		dir := repo(t, c.start)
		if c.files == nil { // the link case
			git(t, dir, "switch", "-q", "-c", "work")
			put(t, dir, map[string]string{"other.yaml": sqlcYAML + "        emit_interface: true\n", "internal/store/querier.go": forgedHeader})
			if err := os.Remove(filepath.Join(dir, "sqlc.yaml")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("other.yaml", filepath.Join(dir, "sqlc.yaml")); err != nil {
				t.Fatal(err)
			}
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-q", "-m", "work")
		} else {
			work(t, dir, c.files)
		}
		code, out := script(t, dir, "", "go-tdd.sh")
		if (code == 0) != c.pass || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want pass=%v and %q\n%s", c.name, code, c.pass, c.want, out)
		}
	}
}

// sqlc-owned.sh prints the files that are what sqlc generates and nothing else.
func TestSqlcOwnedListsOnlyFilesThatAreByteForByteWhatSqlcGenerates(t *testing.T) {
	start := sqlcLeaf(t)
	dir := repo(t, start)
	work(t, dir, map[string]string{"internal/store/querier.go": forgedHeader, "internal/store/extra.go": forgedHeader})
	code, out := script(t, dir, "", "sqlc-owned.sh")
	got := strings.Fields(out)
	want := []string{"internal/store/db.go", "internal/store/info.sql.go", "internal/store/models.go"}
	if code != 0 || strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("exit %d, owned %v, want %v\n%s", code, got, want, out)
	}
	// An edit in the working tree that is not committed yet takes the file out of the list.
	put(t, dir, map[string]string{"internal/store/models.go": start["internal/store/models.go"] + "\nvar X = 1\n"})
	if _, out := script(t, dir, "", "sqlc-owned.sh"); strings.Contains(out, "models.go") {
		t.Errorf("an edited models.go is listed:\n%s", out)
	}
	// With no sqlc.yaml at the base, nothing is owned.
	if code, out := script(t, repo(t, map[string]string{"go.mod": goMod}), "", "sqlc-owned.sh"); code != 0 || strings.TrimSpace(out) != "" {
		t.Errorf("a leaf with no sqlc.yaml: exit %d\n%s", code, out)
	}
}

// capture is a golangci-lint stand-in that keeps the configuration it was called with.
func capture(t *testing.T) string {
	t.Helper()
	bin := stubs(t, "golangci-lint")
	body := "#!/bin/sh\necho \"golangci-lint $*\" >> \"$(dirname \"$0\")/calls.log\"\n" +
		"while [ $# -gt 0 ]; do [ \"$1\" = --config ] && cp \"$2\" \"$(dirname \"$0\")/used.yml\"; shift; done\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "golangci-lint"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// The linters run with the configuration baked into the image, whatever the leaf holds, plus a skip for
// the files verified as sqlc's output (B-049).
func TestStaticLintsWithTheBakedConfigAndSkipsOnlyVerifiedSqlcOutput(t *testing.T) {
	baked, err := os.ReadFile(filepath.Join("devcontainer", "checks", "config", "golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	start := sqlcLeaf(t)
	start[".golangci.yml"] = "version: \"2\"\nlinters:\n  default: none\n"
	dir := repo(t, start)
	work(t, dir, map[string]string{"internal/store/querier.go": forgedHeader})
	bin := capture(t)
	if code, out := script(t, dir, bin, "go-static.sh"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	used, _ := os.ReadFile(filepath.Join(bin, "used.yml"))
	if !strings.HasPrefix(string(used), string(baked)) {
		t.Errorf("the lint did not get the baked config:\n%s", used)
	}
	if strings.Contains(string(used), "default: none") {
		t.Errorf("the lint got the leaf's own config:\n%s", used)
	}
	for _, f := range []string{"db.go", "models.go", "info.sql.go"} {
		if !strings.Contains(string(used), `^internal/store/`+strings.ReplaceAll(f, ".", `\.`)+`$`) {
			t.Errorf("the config does not skip the generated %s:\n%s", f, used)
		}
	}
	if strings.Contains(string(used), "querier") || strings.Contains(string(used), "extra") {
		t.Errorf("the config skips a file sqlc did not write:\n%s", used)
	}
}

// The bypasses of B-049, each with the real linter: a config the leaf swapped, hid or edited is not read,
// and a handwritten file with sqlc's header is linted.
func TestStaticLintsWhatTheLeafTriedToHide(t *testing.T) {
	lint := needTools(t, "golangci-lint")
	bad := map[string]string{"go.mod": goMod, "a.go": "package t\n\nimport \"os\"\n\nfunc A() { os.Remove(\"x\") }\n", "permissive.yml": "version: \"2\"\nlinters:\n  default: none\n  enable:\n    - govet\n"}
	good := map[string]string{"go.mod": goMod, "a.go": aGo, "permissive.yml": bad["permissive.yml"]}
	run := func(t *testing.T, dir string) (int, string) {
		t.Helper()
		return script(t, dir, lint, "go-static.sh")
	}
	t.Run("the config replaced by a link to a permissive one", func(t *testing.T) {
		start := map[string]string{"go.mod": bad["go.mod"], "a.go": bad["a.go"], ".golangci.yml": "version: \"2\"\n"}
		dir := repo(t, start)
		git(t, dir, "switch", "-q", "-c", "work")
		put(t, dir, map[string]string{"permissive.yml": bad["permissive.yml"]})
		if err := os.Remove(filepath.Join(dir, ".golangci.yml")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("permissive.yml", filepath.Join(dir, ".golangci.yml")); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "add", "-A")
		git(t, dir, "commit", "-q", "-m", "link")
		if code, out := run(t, dir); code == 0 || !strings.Contains(out, "errcheck") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
	t.Run("a permissive config hidden by .gitignore", func(t *testing.T) {
		dir := repo(t, map[string]string{"go.mod": bad["go.mod"], "a.go": bad["a.go"], ".gitignore": ".golangci.json\n"})
		git(t, dir, "switch", "-q", "-c", "work")
		put(t, dir, map[string]string{".golangci.json": "{\"version\": \"2\", \"linters\": {\"default\": \"none\", \"enable\": [\"govet\"]}}\n"})
		if code, out := run(t, dir); code == 0 || !strings.Contains(out, "errcheck") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
	t.Run("the config edited", func(t *testing.T) {
		dir := repo(t, map[string]string{"go.mod": bad["go.mod"], "a.go": bad["a.go"], ".golangci.yml": "version: \"2\"\n"})
		git(t, dir, "switch", "-q", "-c", "work")
		put(t, dir, map[string]string{".golangci.yml": bad["permissive.yml"]})
		if code, out := run(t, dir); code == 0 || !strings.Contains(out, "errcheck") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
	t.Run("a handwritten querier.go with a forged sqlc header", func(t *testing.T) {
		start := sqlcLeaf(t)
		dir := repo(t, start)
		work(t, dir, map[string]string{"internal/store/querier.go": forgedHeader})
		if code, out := run(t, dir); code == 0 || !strings.Contains(out, "errcheck") || !strings.Contains(out, "querier.go") {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
	t.Run("clean code with a permissive config beside it passes", func(t *testing.T) {
		dir := repo(t, good)
		git(t, dir, "switch", "-q", "-c", "work")
		if code, out := run(t, dir); code != 0 {
			t.Errorf("exit %d\n%s", code, out)
		}
	})
}

// The unchanged template passes the rungs that read the config, with the real tools.
func TestTheUnchangedTemplatePassesStaticAndTDDAndItsSqlcOutputIsOwned(t *testing.T) {
	bin := needTools(t, "golangci-lint", "sqlc")
	dir := copyTemplate(t, "api")
	needModules(t, dir)
	r := repo(t, readTree(t, dir))
	git(t, r, "switch", "-q", "-c", "work")
	if code, out := script(t, r, bin, "go-static.sh"); code != 0 {
		t.Errorf("go-static.sh: exit %d\n%s", code, out)
	}
	if code, out := script(t, r, bin, "go-tdd.sh"); code != 0 {
		t.Errorf("go-tdd.sh: exit %d\n%s", code, out)
	}
	_, out := script(t, r, bin, "sqlc-owned.sh")
	for _, f := range []string{"internal/store/db.go", "internal/store/models.go", "internal/store/info.sql.go"} {
		if !strings.Contains(out, f) {
			t.Errorf("the template's %s is not listed as sqlc's:\n%s", f, out)
		}
	}
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// The one source of truth for the lint config is the baked one; the template carries an identical copy, and
// neither names a path (the paths of sqlc's output are found by running sqlc).
func TestTheTemplatesLintConfigIsTheBakedOne(t *testing.T) {
	baked, err := os.ReadFile(filepath.Join("devcontainer", "checks", "config", "golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := os.ReadFile(filepath.Join("templates", "api", ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baked, tmpl) {
		t.Error("templates/api/.golangci.yml differs from devcontainer/checks/config/golangci.yml; the baked one is the source, copy it")
	}
	if strings.Contains(string(baked), "paths:") {
		t.Error("the baked config names paths; go-static.sh adds the files sqlc wrote")
	}
	if !strings.Contains(string(baked), "generated: disable") {
		t.Error("the baked config trusts a Code generated header")
	}
}

// static still refuses a change to the dev container; it no longer reads any other config of the leaf.
func TestStaticRefusesAChangeToTheDevContainer(t *testing.T) {
	start := map[string]string{"go.mod": goMod, "a.go": "package t\n", ".devcontainer/devcontainer.json": "{}\n"}
	cases := []struct {
		name  string
		files map[string]string
		pass  bool
	}{
		{"the dev container changed", map[string]string{".devcontainer/devcontainer.json": "{\"x\": 1}\n"}, false},
		{"a file added to the dev container", map[string]string{".devcontainer/extra.json": "{}\n"}, false},
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
	dir := repo(t, start)
	git(t, dir, "switch", "-q", "-c", "work")
	put(t, dir, map[string]string{".devcontainer/new.json": "{}\n"})
	if code, out := script(t, dir, stubs(t, "golangci-lint"), "go-static.sh"); code == 0 || !strings.Contains(out, ".devcontainer/new.json") {
		t.Errorf("an untracked file in the dev container: exit %d\n%s", code, out)
	}
}

// go-contract.sh runs sqlc diff on the sqlc.yaml of the commit the task started from.
func TestContractDiffsAgainstTheBaseSqlcConfig(t *testing.T) {
	dir := repo(t, map[string]string{"openapi.yaml": "openapi: 3.1.0\n", "sqlc.yaml": "base config\n", "other.yaml": "other config\n"})
	git(t, dir, "switch", "-q", "-c", "work")
	if err := os.Remove(filepath.Join(dir, "sqlc.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other.yaml", filepath.Join(dir, "sqlc.yaml")); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "link")
	bin := stubs(t, "vacuum", "sqlc", "oasdiff")
	body := "#!/bin/sh\necho \"sqlc $*\" >> \"$(dirname \"$0\")/calls.log\"\ncp sqlc.yaml \"$(dirname \"$0\")/seen.yaml\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "sqlc"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, out := script(t, dir, bin, "go-contract.sh", "STUB_OASDIFF_OUT=[]"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	seen, _ := os.ReadFile(filepath.Join(bin, "seen.yaml"))
	if string(seen) != "base config\n" {
		t.Errorf("sqlc diff read %q, not the sqlc.yaml of the base commit", seen)
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

// The product's CI is a protected path too: an agent in a run must not weaken the checks the product's
// people rely on. The refusal comes before the lint step, so no tool beyond go and git is needed.
func TestStaticRefusesAWorkflowChange(t *testing.T) {
	const wf = ".github/workflows/ci.yml"
	start := map[string]string{"go.mod": goMod, "a.go": "package t\n", wf: "name: ci\n"}
	cases := []struct {
		name  string
		start map[string]string
		files map[string]string
	}{
		{"a workflow added", map[string]string{"go.mod": goMod, "a.go": "package t\n"}, map[string]string{wf: "name: ci\n"}},
		{"a workflow changed", start, map[string]string{wf: "name: weaker\n"}},
	}
	for _, c := range cases {
		dir := repo(t, c.start)
		work(t, dir, c.files)
		bin := stubs(t, "golangci-lint")
		code, out := script(t, dir, bin, "go-static.sh")
		if code == 0 || !strings.Contains(out, "protected") || !strings.Contains(out, wf) || calls(bin) != "" {
			t.Errorf("%s: exit %d, calls %q\n%s", c.name, code, calls(bin), out)
		}
	}
}

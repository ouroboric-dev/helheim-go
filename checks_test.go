package helheim

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
	if bin != "" {
		cmd.Env = append(cmd.Env, "PATH="+bin+":"+os.Getenv("PATH"))
	}
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

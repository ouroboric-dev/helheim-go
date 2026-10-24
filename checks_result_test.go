package helheim

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ouroboric-dev/gungnir/schema"
)

// Every check of the ladder writes a typed result, ygg.check/v1, when the caller asks for one with
// YGG_CHECK_RESULT, and writes none (and behaves as it always did) when it does not. The scripts hand the machine
// output of their tools to ygg-check-result, the converter of the work image. These tests run the scripts on the
// leaves under testdata/checks/<check>/{pass,fail}/ with the real go, gofmt, git, ygg-deps and ygg-authz and the
// real converter, which is found in the gleipnir module at the pinned version (it is not a dependency of this
// module: go mod download gives its folder, and a wrapper that runs it with node goes on the scripts' PATH).
// The tools that only the image has (golangci-lint, sqlc, vacuum, oasdiff, govulncheck, osv-scanner) are
// recorded stand-ins, made from the files beside each leaf under fake/ (see fakeTools).

const gleipnirPin = "github.com/ouroboric-dev/gleipnir@v0.0.0-20261024192006-9da064f0b1ee"

var (
	converterOnce sync.Once
	converterFile string
	converterErr  error
)

// converterWrapper writes ygg-check-result into bin: a script that runs the converter of the pinned gleipnir with node.
func converterWrapper(t *testing.T, bin string) {
	t.Helper()
	converterOnce.Do(func() {
		cmd := exec.Command("go", "mod", "download", "-json", gleipnirPin)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
		out, err := cmd.Output()
		if err != nil {
			converterErr = &buildError{err, string(out)}
			return
		}
		var m struct{ Dir string }
		if converterErr = json.Unmarshal(out, &m); converterErr == nil {
			converterFile = filepath.Join(m.Dir, "sandbox", "assets", "ygg-check-result.mjs")
			_, converterErr = os.Stat(converterFile)
		}
	})
	if converterErr != nil {
		t.Fatalf("the converter of %s: %v", gleipnirPin, converterErr)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatalf("node is needed to run the converter: %v", err)
	}
	writeExe(t, filepath.Join(bin, "ygg-check-result"), "#!/bin/sh\necho \"ygg-check-result $*\" >> \"$(dirname \"$0\")/calls.log\"\nexec node '"+converterFile+"' \"$@\"\n")
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// fakeTools makes a stand-in for each tool that has files in dir: <tool>.json is what it prints in its machine mode
// (an argument -json or json), <tool>.out and <tool>.err what it prints on stdout and stderr otherwise, <tool>.exit
// the code of that run, <tool>.jsonexit the code of the machine run (0 when absent: govulncheck and go vet exit 0 with
// findings). A tool called with --output.json.path FILE writes <tool>.json to FILE, as golangci-lint does, and
// prints its text. Every call is logged to calls.log in bin.
func fakeTools(t *testing.T, bin, dir string) {
	t.Helper()
	tools := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		tools[strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))] = true
	}
	for tool := range tools {
		base := filepath.Join(dir, tool)
		abs, _ := filepath.Abs(base)
		body := `#!/bin/sh
echo "` + tool + ` $*" >> "$(dirname "$0")/calls.log"
f='` + abs + `'
mode=text
prev=
for a in "$@"; do
  case "$a" in -json|json) mode=json ;; esac
  [ "$prev" != --output.json.path ] || { [ ! -f "$f.json" ] || cp "$f.json" "$a"; }
  prev=$a
done
code() { if [ -f "$f.$1" ]; then cat "$f.$1"; else echo 0; fi; }
if [ "$mode" = json ]; then
  [ ! -f "$f.json" ] || cat "$f.json"
  exit "$(code jsonexit)"
fi
[ ! -f "$f.out" ] || cat "$f.out"
[ ! -f "$f.err" ] || cat "$f.err" >&2
exit "$(code exit)"
`
		writeExe(t, filepath.Join(bin, tool), body)
	}
}

// checkLeaf builds the leaf of a fixture: its start/ on main, and its work/ (when there is one) on a branch.
func checkLeaf(t *testing.T, check, name string) (dir, fake string) {
	t.Helper()
	root := filepath.Join("testdata", "checks", check, name)
	dir = repo(t, fixtureTree(t, filepath.Join(root, "start")))
	if _, err := os.Stat(filepath.Join(root, "work")); err == nil {
		work(t, dir, fixtureTree(t, filepath.Join(root, "work")))
	}
	return dir, filepath.Join(root, "fake")
}

// fixtureTree reads a folder of a fixture as a leaf's files. A file that is meant to be bad Go (a.go.unformatted)
// is kept under a name that keeps gofmt and the tools of this module away from it.
func fixtureTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for name, body := range readTree(t, dir) {
		files[strings.TrimSuffix(name, ".unformatted")] = body
	}
	return files
}

// typed runs a check script in dir and returns its exit code, its output and the result it wrote (nil when it wrote none).
func typed(t *testing.T, dir, bin, name string, env ...string) (int, string, *schema.CheckResult) {
	t.Helper()
	res := filepath.Join(t.TempDir(), "check.json")
	code, out := script(t, dir, bin, name, append([]string{"YGG_CHECK_RESULT=" + res}, env...)...)
	b, err := os.ReadFile(res)
	if err != nil {
		return code, out, nil
	}
	r, err := schema.Decode[schema.CheckResult](schema.ContractCheck, b)
	if err != nil {
		t.Fatalf("%s wrote a result that is not ygg.check/v1: %v\n%s", name, err, b)
	}
	return code, out, &r
}

type resultCase struct {
	check, script string
	setup         func(t *testing.T, bin string) []string // extra tools for bin; returns extra environment
	failKind      string                                  // the kind of the first failure of the failing leaf
	failWant      string                                  // a text that failure holds (message, test or location)
	failTest      string                                  // its test id, when it has one
	passedTest    string                                  // a test the passing leaf's result lists as passed
}

func resultCases() []resultCase {
	noEnv := func(t *testing.T, bin string) []string { return nil }
	return []resultCase{
		{check: "static", script: "go-static.sh", setup: noEnv, failKind: "lint", failWant: "printf"},
		{check: "unit", script: "go-unit.sh", setup: noEnv, failKind: "test", failWant: "A", failTest: "t.TestA", passedTest: "t.TestA"},
		{check: "tdd", script: "go-tdd.sh", setup: noEnv, failKind: "policy", failWant: "b.go"},
		{check: "contract", script: "go-contract.sh", setup: noEnv, failKind: "build", failWant: "db/queries/info.sql:1:1"},
		{check: "deps", script: "go-deps.sh", setup: func(t *testing.T, bin string) []string { copyDepsTool(t, bin); return nil }, failKind: "policy", failWant: "replace example.com/x"},
		{check: "security", script: "go-security.sh", setup: noEnv, failKind: "scan", failWant: "GO-2025-3503"},
		{check: "authz", script: "go-authz.sh", setup: func(t *testing.T, bin string) []string { copyAuthzTool(t, bin); return nil }, failKind: "policy", failWant: "TestCrossTenantGetPhoto"},
		{check: "integration", script: "go-integration.sh", setup: func(t *testing.T, bin string) []string { return databaseOn(t) }, failKind: "test", failWant: "no table", failTest: "t.TestDB", passedTest: "t.TestDB"},
	}
}

// databaseOn listens where go-integration.sh waits for Postgres and returns the environment that points it there.
func databaseOn(t *testing.T) []string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return []string{"YGG_DB_HOST=127.0.0.1", "YGG_DB_PORT=" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)}
}

// copyAuthzTool puts ygg-authz, built as the Containerfile builds it, into bin. The build is not optional here.
func copyAuthzTool(t *testing.T, bin string) {
	t.Helper()
	t.Setenv("YGG_REQUIRE_TEMPLATES", "1")
	b, err := os.ReadFile(filepath.Join(authzTool(t), "ygg-authz"))
	if err != nil {
		t.Fatal(err)
	}
	writeExe(t, filepath.Join(bin, "ygg-authz"), string(b))
}

// prepare makes the leaf and the tools of a case; the leaf of authz gets the authztest package of the template.
func prepare(t *testing.T, c resultCase, name string) (dir, bin string, env []string) {
	t.Helper()
	dir, fake := checkLeaf(t, c.check, name)
	bin = t.TempDir()
	converterWrapper(t, bin)
	fakeTools(t, bin, fake)
	env = c.setup(t, bin)
	if c.check == "authz" {
		src := mustRead(t, filepath.Join("templates", "api", "internal", "authztest", "authztest.go"))
		put(t, dir, map[string]string{"internal/authztest/check.go": src})
	}
	return dir, bin, env
}

func TestEveryCheckWritesATypedResult(t *testing.T) {
	for _, c := range resultCases() {
		t.Run(c.check, func(t *testing.T) {
			// A passing leaf: exit 0, a result that passed, under the check's name in pack.yaml.
			dir, bin, env := prepare(t, c, "pass")
			code, out, r := typed(t, dir, bin, c.script, env...)
			if code != 0 {
				t.Fatalf("the passing leaf: exit %d\n%s", code, out)
			}
			if r == nil || !r.Passed || r.Check != c.check || r.ExitCode != 0 || r.TimedOut || len(r.Failures) != 0 {
				t.Fatalf("the passing leaf: result %+v\n%s", r, out)
			}
			if c.passedTest != "" && !contains(r.PassedTests, c.passedTest) {
				t.Errorf("the passing leaf's result lists %v as passed, want %s", r.PassedTests, c.passedTest)
			}

			// A failing leaf: a non-zero exit, a result that did not pass, with the failure the tool found.
			dir, bin, env = prepare(t, c, "fail")
			code, out, r = typed(t, dir, bin, c.script, env...)
			if code == 0 {
				t.Fatalf("the failing leaf passed\n%s", out)
			}
			if r == nil || r.Passed || r.Check != c.check || r.ExitCode != code || len(r.Failures) == 0 {
				t.Fatalf("the failing leaf: exit %d, result %+v\n%s", code, r, out)
			}
			f := r.Failures[0]
			if f.Kind != c.failKind || f.Test != c.failTest || !strings.Contains(f.Message+" "+f.Test+" "+f.Location, c.failWant) {
				t.Errorf("the first failure is %+v, want kind %s, test %q and %q in it", f, c.failKind, c.failTest, c.failWant)
			}

			// Asked for no result, the same script writes none and ends the same way.
			dir, bin, env = prepare(t, c, "fail")
			code2, out2 := script(t, dir, bin, c.script, env...)
			if code2 != code {
				t.Errorf("without YGG_CHECK_RESULT the failing leaf exits %d, with it %d\n%s", code2, code, out2)
			}
			if strings.Contains(calls(bin), "ygg-check-result") {
				t.Errorf("the converter ran although no result was asked for: %s", calls(bin))
			}
		})
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// A converter that cannot do its work is a failure of the realm, and the script says so by failing, even when the
// tool passed; a script that failed keeps its own code.
func TestAScriptFailsWhenTheConverterDoes(t *testing.T) {
	for _, name := range []string{"pass", "fail"} {
		c := resultCases()[1] // unit
		dir, bin, env := prepare(t, c, name)
		writeExe(t, filepath.Join(bin, "ygg-check-result"), "#!/bin/sh\necho 'ygg-check-result: broken' >&2\nexit 2\n")
		code, out, r := typed(t, dir, bin, c.script, env...)
		if code == 0 || r != nil || !strings.Contains(out, "ygg-check-result: broken") {
			t.Errorf("%s leaf, broken converter: exit %d, result %v\n%s", name, code, r, out)
		}
	}
}

func caseOf(t *testing.T, check string) resultCase {
	t.Helper()
	for _, c := range resultCases() {
		if c.check == check {
			return c
		}
	}
	t.Fatalf("no check %s", check)
	return resultCase{}
}

// A check with several steps ends at the first that fails, and its result is made from that step's tool: the
// first failure is the one the step's tool found, in the kind the converter gives it.
func TestTheResultIsMadeFromTheStepThatFailed(t *testing.T) {
	cases := []struct {
		check, leaf string
		kind, want  string
	}{
		{"static", "gofmt", "lint", "a.go is not gofmt-clean"},
		{"static", "protected", "policy", ".golangci.yml configures the checks"},
		{"static", "typeerror", "build", "undefinedThing"},
		{"static", "golangci", "lint", "errcheck: Error return value of `os.Remove` is not checked"},
		{"tdd", "parent", "policy", "again_test.go passes on the commit the task started from"},
		{"contract", "lint", "lint", "the document has no info object"},
		{"contract", "stale", "build", "the code in internal/store is not what the SQL says"},
		{"deps", "audit", "scan", "GO-2025-3503"},
		{"security", "osv", "scan", "golang.org/x/text@0.3.6: GHSA-ppp9-7jff-5vj2"},
		{"authz", "failing", "test", "user B got the photo"},
		{"integration", "build", "build", "undefinedThing"},
	}
	for _, c := range cases {
		t.Run(c.check+"/"+c.leaf, func(t *testing.T) {
			rc := caseOf(t, c.check)
			dir, bin, env := prepare(t, rc, c.leaf)
			code, out, r := typed(t, dir, bin, rc.script, env...)
			if code == 0 || r == nil || r.Passed || r.Check != c.check || r.ExitCode != code || len(r.Failures) == 0 {
				t.Fatalf("exit %d, result %+v\n%s", code, r, out)
			}
			f := r.Failures[0]
			if f.Kind != c.kind || !strings.Contains(f.Message, c.want) {
				t.Errorf("the first failure is %+v, want kind %s and %q in the message", f, c.kind, c.want)
			}
		})
	}
}

// The module cache that does not match go.sum ends every Go check with a policy finding of its own.
func TestACheckThatFindsTheModuleCacheTamperedWithSaysSo(t *testing.T) {
	c := caseOf(t, "unit")
	dir, bin, env := prepare(t, c, "pass")
	cache := t.TempDir()
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", cache).Run() })
	// A requirement that go.sum does not vouch for: go mod verify fails before it reaches the network.
	put(t, dir, map[string]string{"go.mod": "module t\n\ngo 1.26\n\nrequire example.invalid/dep v1.0.0\n"})
	code, out, r := typed(t, dir, bin, c.script, append(env, "GOMODCACHE="+cache, "GOPROXY=off", "GOFLAGS=-mod=mod")...)
	if code == 0 || r == nil || len(r.Failures) != 1 || r.Failures[0].Kind != "policy" || !strings.Contains(r.Failures[0].Message, "module cache does not match go.sum") {
		t.Errorf("exit %d, result %+v\n%s", code, r, out)
	}
}

// ygg_result, which every script ends with, on its own: the exit code is the script's, a timeout is said so, a
// converter that fails fails a script that passed.
func TestTheResultLibraryKeepsTheExitCodeAndSaysATimeout(t *testing.T) {
	lib, err := filepath.Abs(filepath.Join("devcontainer", "checks", "lib", "result.sh"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(bin, body string, env ...string) (int, string) {
		cmd := exec.Command("bash", "-c", "set -euo pipefail\n. '"+lib+"'\nygg_start unit\n"+body)
		cmd.Env = append(os.Environ(), append([]string{"PATH=" + bin + ":" + os.Getenv("PATH")}, env...)...)
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), string(out)
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0, string(out)
	}
	bin := t.TempDir()
	writeExe(t, filepath.Join(bin, "ygg-check-result"), "#!/bin/sh\necho \"$*\" >> \"$(dirname \"$0\")/calls.log\"\n")
	if code, out := run(bin, "exit 124", "YGG_CHECK_RESULT=/x"); code != 124 || !strings.Contains(calls(bin), "--timed-out") || !strings.Contains(calls(bin), "--exit 124") {
		t.Errorf("a timeout: exit %d, converter called %q\n%s", code, calls(bin), out)
	}
	bin = t.TempDir()
	writeExe(t, filepath.Join(bin, "ygg-check-result"), "#!/bin/sh\necho \"$*\" >> \"$(dirname \"$0\")/calls.log\"\n")
	if code, out := run(bin, "ygg_stage none policy 'one' lint 'two'\nexit 3", "YGG_CHECK_RESULT=/x"); code != 3 ||
		!strings.Contains(calls(bin), "--check unit --exit 3 --duration-ms") || !strings.Contains(calls(bin), "--format none --kind policy --message one --kind lint --message two") {
		t.Errorf("two messages: exit %d, converter called %q\n%s", code, calls(bin), out)
	}
	if code, out := run(bin, "true"); code != 0 || strings.Count(calls(bin), "\n") != 1 {
		t.Errorf("no result asked for: exit %d, converter called %q\n%s", code, calls(bin), out)
	}
	writeExe(t, filepath.Join(bin, "ygg-check-result"), "#!/bin/sh\nexit 2\n")
	if code, _ := run(bin, "true", "YGG_CHECK_RESULT=/x"); code == 0 {
		t.Errorf("a converter that fails left a passing script passing")
	}
	if code, _ := run(bin, "exit 5", "YGG_CHECK_RESULT=/x"); code != 5 {
		t.Errorf("a converter that fails changed the code of a failing script: %d", code)
	}
}

// A script that exits 0 writes a passing result, whatever its machine runs said: a tool whose machine mode printed
// nothing, or noise on stderr, while its text mode passed, must not leave a staged failure behind.
func TestAPassingScriptWritesAPassingResultWhateverTheMachineModeSaid(t *testing.T) {
	cases := []struct{ check, leaf string }{{"security", "nojson"}, {"deps", "nojson"}, {"static", "vetnoise"}}
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.check+"/"+c.leaf, func(t *testing.T) {
			rc := caseOf(t, c.check)
			dir, bin, env := prepare(t, rc, c.leaf)
			if c.check == "static" {
				// go vet -json prints noise on stderr and nothing on stdout; every other call is the real go.
				writeExe(t, filepath.Join(bin, "go"), "#!/bin/sh\nif [ \"$1 $2\" = 'vet -json' ]; then echo 'vet: noise' >&2; exit 0; fi\nexec '"+realGo+"' \"$@\"\n")
			}
			code, out, r := typed(t, dir, bin, rc.script, env...)
			if code != 0 || r == nil || !r.Passed || r.ExitCode != 0 || len(r.Failures) != 0 {
				t.Fatalf("exit %d, result %+v\n%s", code, r, out)
			}
		})
	}
}

// A script that dies before it reaches a tool still says so.
func TestAScriptThatDiesBeforeATool(t *testing.T) {
	lib, _ := filepath.Abs(filepath.Join("devcontainer", "checks", "lib", "result.sh"))
	bin := t.TempDir()
	writeExe(t, filepath.Join(bin, "ygg-check-result"), "#!/bin/sh\necho \"$*\" >> \"$(dirname \"$0\")/calls.log\"\n")
	cmd := exec.Command("bash", "-c", "set -euo pipefail\n. '"+lib+"'\nygg_start unit\nfalse\n")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "YGG_CHECK_RESULT=/x")
	_ = cmd.Run()
	if !strings.Contains(calls(bin), "--kind other --message the script exited 1 before it reached a tool") {
		t.Errorf("converter called %q", calls(bin))
	}
}

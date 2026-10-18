package helheim

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// go-deps.sh is rung 4's first half: the dependency policy (B-049 style: the policy is baked into the image and
// passed to the checker by path; the leaf's own copy of it, if it has one, is never read). These tests run it on
// small leaves with the real go, and stand-ins for govulncheck and osv-scanner.

const goModWith = "module t\n\ngo 1.26\n\n"

func TestDepsPassesACleanLeafAndRunsTheAuditsThePolicyNames(t *testing.T) {
	bin := stubs(t, "govulncheck", "osv-scanner")
	code, out := script(t, leaf(t, map[string]string{"go.mod": goMod}), bin, "go-deps.sh")
	if code != 0 || !strings.Contains(out, "deps policy: ok") {
		t.Fatalf("a clean leaf: exit %d\n%s", code, out)
	}
	for _, want := range []string{"govulncheck ./...", "govulncheck -tags integration ./...", "osv-scanner scan source -L go.mod"} {
		if !strings.Contains(calls(bin), want) {
			t.Errorf("calls %q lack %q", calls(bin), want)
		}
	}
	for _, env := range []string{"STUB_GOVULNCHECK_EXIT=3", "STUB_OSV_SCANNER_EXIT=1"} {
		if code, _ := script(t, leaf(t, map[string]string{"go.mod": goMod}), stubs(t, "govulncheck", "osv-scanner"), "go-deps.sh", env); code == 0 {
			t.Errorf("%s passed", env)
		}
	}
}

func TestDepsRefusesWhatIsNotPinnedToATaggedVersionAndWhatIsBelowTheMinimum(t *testing.T) {
	cases := []struct{ name, mod, want string }{
		{"a pseudo-version", "require example.invalid/dep v0.0.0-20261017195515-cb1dc0ee9cc6\n", "example.invalid/dep v0.0.0-20261017195515-cb1dc0ee9cc6 is a pseudo-version"},
		{"a pre-release", "require example.invalid/dep v1.2.3-rc.1\n", "example.invalid/dep v1.2.3-rc.1 is a pre-release"},
		{"a replace to a folder", "require example.invalid/dep v1.2.3\nreplace example.invalid/dep => ../dep\n", "replace example.invalid/dep"},
		{"a replace to another module", "require example.invalid/dep v1.2.3\nreplace example.invalid/dep => example.invalid/fork v1.2.3\n", "replace example.invalid/dep"},
		{"x/text below the minimum", "require golang.org/x/text v0.41.0 // indirect\n", "golang.org/x/text v0.41.0 is below the minimum 0.42.0"},
		{"a pre-release of the minimum", "require golang.org/x/text v0.42.0-rc.1 // indirect\n", "golang.org/x/text v0.42.0-rc.1 is below the minimum 0.42.0"},
		{"x/text far below the minimum", "require golang.org/x/text v0.9.0\n", "golang.org/x/text v0.9.0 is below the minimum 0.42.0"},
	}
	for _, c := range cases {
		bin := stubs(t, "govulncheck", "osv-scanner")
		code, out := script(t, leaf(t, map[string]string{"go.mod": goModWith + c.mod}), bin, "go-deps.sh")
		if code == 0 || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want %q in:\n%s", c.name, code, c.want, out)
		}
		if calls(bin) != "" {
			t.Errorf("%s: an audit tool ran after the policy failed: %s", c.name, calls(bin))
		}
	}
	// At the minimum and above it, in any of the forms a version has, the policy is met.
	for _, v := range []string{"v0.42.0", "v0.42.1", "v0.43.0", "v1.0.0"} {
		dir := leaf(t, map[string]string{"go.mod": goModWith + "require golang.org/x/text " + v + " // indirect\n"})
		if out, err := checker(t, dir, "check", filepath.Join("devcontainer", "checks", "config", "deps-policy.json")); err != nil || !strings.Contains(out, "deps policy: ok") {
			t.Errorf("x/text %s: %v\n%s", v, err, out)
		}
	}
}

func TestDepsListsEveryProblemAndNeverReadsAPolicyOfTheLeaf(t *testing.T) {
	files := map[string]string{
		"go.mod": goModWith + "require (\n\texample.invalid/a v0.0.0-20261017195515-cb1dc0ee9cc6\n\tgolang.org/x/text v0.41.0 // indirect\n)\nreplace example.invalid/a => ../a\n",
		// the leaf's own policy, which would pass all of the above
		"deps-policy.json": `{"schema":"ygg.deps/v1","pin":"minor","minimum":{},"audit":[]}`,
		"devcontainer/checks/config/deps-policy.json": `{"schema":"ygg.deps/v1","pin":"minor","minimum":{},"audit":[]}`,
	}
	code, out := script(t, leaf(t, files), stubs(t, "govulncheck", "osv-scanner"), "go-deps.sh")
	if code == 0 {
		t.Fatalf("a leaf with its own policy passed:\n%s", out)
	}
	for _, want := range []string{"is a pseudo-version", "replace example.invalid/a", "is below the minimum 0.42.0", "3 problem(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

// checker runs devcontainer/checks/lib/godeps.go in dir with the command and the path of a policy.
func checker(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	helper, err := filepath.Abs(filepath.Join("devcontainer", "checks", "lib", "godeps.go"))
	if err != nil {
		t.Fatal(err)
	}
	if p, err := filepath.Abs(args[len(args)-1]); err == nil {
		args[len(args)-1] = p
	}
	cmd := exec.Command("go", append([]string{"run", helper}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The checker reads the policy it is given and refuses one it does not know.
func TestDepsCheckerRefusesAPolicyItDoesNotKnow(t *testing.T) {
	dir := leaf(t, map[string]string{"go.mod": goMod})
	for name, policy := range map[string]string{
		"another schema":   `{"schema":"ygg.deps/v2","pin":"minor","minimum":{},"audit":[]}`,
		"another pin":      `{"schema":"ygg.deps/v1","pin":"major","minimum":{},"audit":[]}`,
		"an unknown audit": `{"schema":"ygg.deps/v1","pin":"minor","minimum":{},"audit":["npm"]}`,
		"a bad minimum":    `{"schema":"ygg.deps/v1","pin":"minor","minimum":{"a/b":"one"},"audit":[]}`,
		"an unknown field": `{"schema":"ygg.deps/v1","pin":"minor","minimum":{},"audit":[],"extra":1}`,
		"not json":         `pin: minor`,
	} {
		p := filepath.Join(t.TempDir(), "policy.json")
		if err := os.WriteFile(p, []byte(policy), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := checker(t, dir, "check", p)
		if err == nil || !strings.Contains(string(out), "deps policy") {
			t.Errorf("%s: err %v\n%s", name, err, out)
		}
	}
}

func TestDepsPassesTheUnchangedTemplateWithTheRealGo(t *testing.T) {
	dir := copyTemplate(t, "api")
	needModules(t, dir)
	code, out := script(t, dir, stubs(t, "govulncheck", "osv-scanner"), "go-deps.sh")
	if code != 0 || !strings.Contains(out, "deps policy: ok") {
		t.Errorf("the template: exit %d\n%s", code, out)
	}
}

func TestTheBakedDepsPolicyIsValidAndTheChecksRunIt(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("devcontainer", "checks", "config", "deps-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema":"ygg.deps/v1"`, `"pin":"minor"`, `"golang.org/x/text":"0.42.0"`, `"govulncheck"`, `"osv"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the baked policy lacks %s", want)
		}
	}
	s, _ := os.ReadFile(filepath.Join("devcontainer", "checks", "go-deps.sh"))
	if !strings.Contains(string(s), `"$here/config/deps-policy.json"`) {
		t.Error("go-deps.sh does not read the policy from from beside itself, through $here")
	}
}

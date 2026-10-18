//go:build ignore

// godeps.go is the dependency policy checker of go-deps.sh (v0-T257). Run it in the leaf's top folder:
//
//	go run godeps.go check <policy.json>   checks go.mod against the policy
//	go run godeps.go audit <policy.json>   prints the audit tools the policy names, one per line
//
// It uses the standard library only, so it needs no module download. The policy is baked into the image
// and the script passes its path; the checker never looks for a policy in the leaf.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type policy struct {
	Schema  string            `json:"schema"`
	Pin     string            `json:"pin"`
	Minimum map[string]string `json:"minimum"`
	Audit   []string          `json:"audit"`
}

type goMod struct {
	Require []struct {
		Path     string
		Version  string
		Indirect bool
	}
	Replace []struct {
		Old struct{ Path string }
	}
}

var (
	release = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(\+incompatible)?$`)
	pseudo  = regexp.MustCompile(`-(0\.)?\d{14}-[0-9a-f]{12}`)
	plain   = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
)

func fail(format string, a ...any) {
	fmt.Printf("deps policy: "+format+"\n", a...)
	os.Exit(1)
}

func load(path string) policy {
	b, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	var p policy
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		fail("%s is not a policy: %v", path, err)
	}
	if p.Schema != "ygg.deps/v1" {
		fail("%s has schema %q, want ygg.deps/v1", path, p.Schema)
	}
	if p.Pin != "minor" {
		fail("%s has pin %q, want minor", path, p.Pin)
	}
	for pkg, v := range p.Minimum {
		if !plain.MatchString(v) {
			fail("%s: the minimum of %s is %q, want x.y.z", path, pkg, v)
		}
	}
	for _, a := range p.Audit {
		if a != "govulncheck" && a != "osv" {
			fail("%s names the audit tool %q; the Go ladder knows govulncheck and osv", path, a)
		}
	}
	return p
}

func parts(v string) [3]int {
	m := release.FindStringSubmatch(v)
	if m == nil {
		m = plain.FindStringSubmatch("v" + strings.TrimPrefix(v, "v"))
		if m == nil {
			m = plain.FindStringSubmatch(strings.TrimPrefix(v, "v"))
		}
	}
	var out [3]int
	if m != nil {
		for i := range out {
			out[i], _ = strconv.Atoi(m[i+1])
		}
	}
	return out
}

func below(have, want string) bool {
	h, w := parts(have), parts(want)
	for i := range h {
		if h[i] != w[i] {
			return h[i] < w[i]
		}
	}
	// a pre-release of the minimum is lower than the minimum
	return strings.Contains(have, "-")
}

func check(p policy) {
	out, err := exec.Command("go", "mod", "edit", "-json").Output()
	if err != nil {
		fail("go mod edit -json: %v", err)
	}
	var m goMod
	if err := json.Unmarshal(out, &m); err != nil {
		fail("go.mod: %v", err)
	}
	var problems []string
	for _, r := range m.Replace {
		problems = append(problems, fmt.Sprintf("go.mod: replace %s: a dependency is taken from nowhere the lock file vouches for", r.Old.Path))
	}
	for _, r := range m.Require {
		if !r.Indirect {
			switch {
			case pseudo.MatchString(r.Version):
				problems = append(problems, fmt.Sprintf("go.mod: %s %s is a pseudo-version; pin a tagged release", r.Path, r.Version))
			case !release.MatchString(r.Version):
				problems = append(problems, fmt.Sprintf("go.mod: %s %s is a pre-release or not a tagged version; pin a tagged release", r.Path, r.Version))
			}
		}
		if min, ok := p.Minimum[r.Path]; ok && below(r.Version, min) {
			problems = append(problems, fmt.Sprintf("go.mod: %s %s is below the minimum %s", r.Path, r.Version, min))
		}
	}
	sort.Strings(problems)
	for _, s := range problems {
		fmt.Println(s)
	}
	if len(problems) > 0 {
		fail("%d problem(s)", len(problems))
	}
	fmt.Println("deps policy: ok")
}

func main() {
	if len(os.Args) != 3 {
		fail("usage: godeps.go check|audit <policy.json>")
	}
	p := load(os.Args[2])
	switch os.Args[1] {
	case "check":
		check(p)
	case "audit":
		for _, a := range p.Audit {
			fmt.Println(a)
		}
	default:
		fail("unknown command %q", os.Args[1])
	}
}

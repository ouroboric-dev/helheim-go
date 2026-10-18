// Command ygg-authz is the reader of go-authz.sh, the two-tenant authorisation harness of the Go ladder
// (spec §9.5 rung 4, v0-T260). The image builds it from this folder, with the hash of its one dependency in
// go.sum, into /usr/local/bin where the agent cannot write.
//
//	ygg-authz plan  -spec openapi.yaml [-registry authz.json]
//	    lists every operation that takes a resource id and the test it needs
//	ygg-authz check -spec openapi.yaml [-registry authz.json] -tests go-test.json
//	    fails unless each of them has a passing TestCrossTenant<OperationId> in which user B got 403 or 404 and
//	    user A got the resource. The answers are lines "YGG-AUTHZ {json}" that authztest.Check printed, which
//	    go test -json carries in the output of the test
//
// A route takes a resource id when a variable of its path is named id, or ends in Id, ID, _id or -id.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type operation struct{ ID, Method, Path, Test string }

var (
	pathVar  = regexp.MustCompile(`\{([^{}]+)\}`)
	idName   = regexp.MustCompile(`^(id|.*(Id|ID|_id|-id))$`)
	opName   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	methods  = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
	registry = struct {
		Schema string            `json:"schema"`
		Public map[string]string `json:"public"`
	}{}
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: ygg-authz plan|check -spec openapi.yaml [-registry authz.json] [-tests go-test.json -report report.jsonl]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	spec := fs.String("spec", "openapi.yaml", "the OpenAPI document")
	reg := fs.String("registry", "", "the registry of public routes")
	tests := fs.String("tests", "", "the output of go test -json")
	_ = fs.Parse(os.Args[2:])
	var more []string // output of failed tests, shown after the problems and not counted
	ops, public, problems := plan(*spec, *reg)
	switch os.Args[1] {
	case "plan":
		for _, o := range ops {
			fmt.Printf("require %s %s %s %s\n", o.ID, o.Method, o.Path, o.Test)
		}
		for _, id := range sortedKeys(public) {
			fmt.Printf("public %s: %s\n", id, public[id])
		}
	case "check":
		if len(problems) == 0 {
			problems, more = check(ops, *tests)
		}
	default:
		fmt.Printf("authz: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 {
		for _, l := range more {
			fmt.Println("  " + l)
		}
		fmt.Printf("authz: %d problem(s)\n", len(problems))
		os.Exit(1)
	}
	if os.Args[1] == "check" {
		fmt.Println("authz: ok")
	}
}

// plan reads the spec and the registry. ops are the operations that need a test, public the ones the registry
// excepts.
func plan(specPath, regPath string) (ops []operation, public map[string]string, problems []string) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, nil, []string{"authz: no " + specPath + ": " + err.Error()}
	}
	var doc struct {
		Paths map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, []string{"authz: " + specPath + " can't be read: " + err.Error()}
	}
	if len(doc.Paths) == 0 {
		return nil, nil, []string{"authz: " + specPath + " has no paths"}
	}
	seen := map[string]bool{}
	all := map[string]bool{}
	for path, node := range doc.Paths {
		var item map[string]yaml.Node
		if err := node.Decode(&item); err != nil {
			problems = append(problems, fmt.Sprintf("authz: %s can't be read: %v", path, err))
			continue
		}
		if _, ref := item["$ref"]; ref {
			problems = append(problems, fmt.Sprintf("authz: %s is a $ref; write the path item in the file, so that its routes can be read", path))
			continue
		}
		resource := false
		for _, m := range pathVar.FindAllStringSubmatch(path, -1) {
			resource = resource || idName.MatchString(m[1])
		}
		for _, m := range methods {
			n, ok := item[m]
			if !ok {
				continue
			}
			var op struct {
				ID string `yaml:"operationId"`
			}
			_ = n.Decode(&op)
			label := strings.ToUpper(m) + " " + path
			switch {
			case op.ID == "" && resource:
				problems = append(problems, fmt.Sprintf("authz: %s has no operationId; the test is named for it", label))
				continue
			case op.ID == "":
				continue
			case !opName.MatchString(op.ID):
				problems = append(problems, fmt.Sprintf("authz: operationId %s of %s must be letters and digits, starting with a letter", op.ID, label))
				continue
			case seen[op.ID]:
				problems = append(problems, fmt.Sprintf("authz: operationId %s is used twice", op.ID))
				continue
			}
			seen[op.ID] = true
			all[op.ID] = resource
			if resource {
				ops = append(ops, operation{op.ID, strings.ToUpper(m), path, "TestCrossTenant" + strings.ToUpper(op.ID[:1]) + op.ID[1:]})
			}
		}
	}
	if regPath != "" {
		public, problems = readRegistry(regPath, all, problems)
	}
	var required []operation
	for _, o := range ops {
		if _, ok := public[o.ID]; !ok {
			required = append(required, o)
		}
	}
	sort.Strings(problems)
	sort.Slice(required, func(i, j int) bool { return required[i].ID < required[j].ID })
	return required, public, problems
}

func readRegistry(path string, all map[string]bool, problems []string) (map[string]string, []string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, append(problems, "authz: "+path+": "+err.Error())
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	r := registry
	if err := dec.Decode(&r); err != nil {
		return nil, append(problems, "authz: "+path+" is not a registry: "+err.Error())
	}
	if r.Schema != "ygg.authz/v1" {
		return nil, append(problems, fmt.Sprintf("authz: %s has schema %q, want ygg.authz/v1", path, r.Schema))
	}
	for id, why := range r.Public {
		switch resource, there := all[id]; {
		case !there:
			problems = append(problems, fmt.Sprintf("authz: %s names %s, which is no operation of the contract; remove it", path, id))
		case !resource:
			problems = append(problems, fmt.Sprintf("authz: %s names %s, which takes no resource id; remove it", path, id))
		case len(strings.Fields(why)) < 2:
			problems = append(problems, fmt.Sprintf("authz: %s gives %s no reason; say why anyone may read it", path, id))
		}
	}
	return r.Public, problems
}

type event struct {
	Action, Package, Test, Output string
}

type entry struct {
	Operation, Test string
	A, B            int
}

func check(ops []operation, testsPath string) (problems, more []string) {
	passed, entries, failedOut, err := readTests(testsPath)
	if err != nil {
		return []string{"authz: " + err.Error()}, nil
	}
	for _, o := range ops {
		label := fmt.Sprintf("%s %s %s", o.ID, o.Method, o.Path)
		var got *entry
		for i := range entries {
			if entries[i].Operation == o.ID && entries[i].Test == o.Test {
				got = &entries[i]
			}
		}
		switch {
		case !passed[o.Test]:
			problems = append(problems, fmt.Sprintf("missing %s: no passing test %s", label, o.Test))
		case got == nil:
			problems = append(problems, fmt.Sprintf("missing %s: %s passed without a request of user B; call authztest.Check with Operation %q", label, o.Test, o.ID))
		case got.A < 200 || got.A > 299:
			problems = append(problems, fmt.Sprintf("missing %s: user A got %d, want 2xx; use a resource of user A", label, got.A))
		case got.B != 403 && got.B != 404:
			problems = append(problems, fmt.Sprintf("missing %s: user B got %d, want 403 or 404", label, got.B))
		default:
			fmt.Printf("covered %s (%s: user A %d, user B %d)\n", o.ID, o.Test, got.A, got.B)
		}
	}
	if len(problems) > 0 {
		more = failedOut
	}
	return problems, more
}

// readTests returns the top-level tests that passed and the output that is not a pass: what failed, and what
// a package that did not build said.
func readTests(path string) (passed map[string]bool, entries []entry, out []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()
	passed = map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		top, _, _ := strings.Cut(e.Test, "/")
		switch {
		case e.Action == "pass" && e.Test != "" && e.Test == top:
			passed[top] = true
		case e.Action == "fail" && e.Test != "":
			delete(passed, top)
		case e.Action == "output" && strings.HasPrefix(e.Output, "YGG-AUTHZ "):
			var en entry
			if json.Unmarshal([]byte(strings.TrimPrefix(e.Output, "YGG-AUTHZ ")), &en) == nil {
				entries = append(entries, en)
			}
		case e.Action == "output" && strings.TrimSpace(e.Output) != "" && !strings.HasPrefix(e.Output, "=== ") &&
			!strings.HasPrefix(e.Output, "--- PASS") && !strings.HasPrefix(e.Output, "PASS") && !strings.HasPrefix(e.Output, "ok ") && !strings.HasPrefix(e.Output, "?") && !strings.HasPrefix(e.Output, "testing: warning"):
			out = append(out, strings.TrimRight(e.Output, "\n"))
		}
	}
	if len(out) > 40 {
		out = append(out[:40], "... more output left out")
	}
	return passed, entries, out, sc.Err()
}

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

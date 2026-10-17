package helheim

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// copyTemplate copies templates/<name> into a temp dir, where it is a module of its own, as it is in a
// product.
func copyTemplate(t *testing.T, name string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(filepath.Join("templates", name))); err != nil {
		t.Fatalf("templates/%s: %v", name, err)
	}
	return dst
}

func toolIn(t *testing.T, dir, tool string, args ...string) string {
	t.Helper()
	cmd := exec.Command(tool, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s in the template: %v\n%s", tool, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func goIn(t *testing.T, dir string, args ...string) string { return toolIn(t, dir, "go", args...) }

func TestTheAPITemplateBuildsAndPassesItsOwnChecks(t *testing.T) {
	dir := copyTemplate(t, "api")
	if out := toolIn(t, dir, "gofmt", "-l", "."); out != "" {
		t.Errorf("not gofmt-clean:\n%s", out)
	}
	goIn(t, dir, "vet", "./...")
	goIn(t, dir, "test", "-race", "-count=1", "./...")
}

func TestATemplateCarriesWhatARepoOfTheFactoryNeeds(t *testing.T) {
	dir := copyTemplate(t, "api")
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("the template lacks %s: %v", name, err)
		}
		return string(b)
	}
	if lic := read("LICENSE"); !strings.HasPrefix(lic, "MIT License") {
		t.Errorf("LICENSE starts %q; a product may use a template under its own licence only if it is MIT", lic[:20])
	}
	if n := strings.Count(read("AGENTS.md"), "\n"); n > 100 {
		t.Errorf("AGENTS.md is %d lines; a short file that points into docs, about 100 lines (spec §4.2)", n)
	}
	for _, name := range []string{"init.sh", "openapi.yaml", ".golangci.yml", ".gitignore"} {
		read(name)
	}
	// The devcontainer.json is for people's editors. The factory never reads it, and it must stay one a
	// person can open without running anything of the repo's.
	var dc map[string]any
	if err := json.Unmarshal([]byte(read(".devcontainer/devcontainer.json")), &dc); err != nil {
		t.Fatalf(".devcontainer/devcontainer.json: %v", err)
	}
	for _, key := range []string{"initializeCommand", "onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand",
		"postAttachCommand", "mounts", "runArgs", "privileged", "capAdd", "securityOpt", "dockerComposeFile", "build", "features", "containerEnv", "remoteEnv", "forwardPorts"} {
		if _, bad := dc[key]; bad {
			t.Errorf(".devcontainer/devcontainer.json sets %s, which runs or widens something when a person opens the repo", key)
		}
	}
}

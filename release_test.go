package helheim

import (
	"bytes"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestReleaseScriptStarts: release.sh builds a static binary and an OUT/run.sh that starts it with no Go on
// the PATH, from any working directory. OUT holds only regular files and folders.
func TestReleaseScriptStarts(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		if os.Getenv("YGG_REQUIRE_TEMPLATES") != "" {
			t.Fatalf("no go toolchain: %v", err)
		}
		t.Skip("no go toolchain (set YGG_REQUIRE_TEMPLATES to fail instead)")
	}
	dir := copyTemplate(t, "api")
	needModules(t, dir)
	if fi, err := os.Stat(filepath.Join(dir, "release.sh")); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("the template needs an executable release.sh: %v", err)
	}
	// OUT is relative to the caller's folder, which is not the leaf. A link planted at OUT/run.sh must not
	// be followed.
	work := t.TempDir()
	out := filepath.Join(work, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(work, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(out, "run.sh")); err != nil {
		t.Fatal(err)
	}
	rel := exec.Command(filepath.Join(dir, "release.sh"))
	rel.Dir = work
	rel.Env = append(os.Environ(), "OUT=out", "GOWORK=off")
	if b, err := rel.CombinedOutput(); err != nil {
		if os.Getenv("YGG_REQUIRE_TEMPLATES") == "" && (strings.Contains(string(b), "dial tcp: lookup") || strings.Contains(string(b), "no such host") || strings.Contains(string(b), "connection refused") || strings.Contains(string(b), "i/o timeout")) {
			t.Skipf("the modules can't be downloaded (set YGG_REQUIRE_TEMPLATES to fail instead): %s", b)
		}
		t.Fatalf("release.sh: %v\n%s", err, b)
	} else if !strings.Contains(string(b), "api: ") || !strings.Contains(string(b), "bytes") {
		t.Errorf("release.sh does not print the binary's size:\n%s", b)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("release.sh wrote through a link planted at OUT/run.sh: %q", b)
	}
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !d.Type().IsRegular() {
			t.Errorf("%s is not a regular file (%v)", p, d.Type())
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(out, "run.sh")); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("OUT/run.sh must be an executable file: %v", err)
	}
	if t.Failed() {
		return
	}

	port := freePort(t)
	var log bytes.Buffer
	cmd := exec.Command(filepath.Join(out, "run.sh"))
	cmd.Dir = t.TempDir() // not the release folder
	cmd.Env = []string{"PATH=/usr/bin:/bin", "PORT=" + port, "HOST=127.0.0.1"}
	cmd.Stdout, cmd.Stderr = &log, &log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	deadline := time.Now().Add(30 * time.Second)
	for {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, time.Second); err == nil {
			_ = c.Close()
			break
		}
		select {
		case err := <-exited:
			t.Fatalf("OUT/run.sh stopped before the port answered: %v\n%s", err, log.String())
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("port %s did not answer within 30 s\n%s", port, log.String())
		}
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Errorf("OUT/run.sh did not stop on SIGTERM")
	}
}

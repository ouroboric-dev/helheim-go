package helheim

import (
	"os"
	"strings"
	"testing"
)

// TestPromptNamesTheRuntime: the implementer prompt and the template's AGENTS.md tell the implementer the
// runtime names of spec §9.4 and the photo rule of §9.3.
func TestPromptNamesTheRuntime(t *testing.T) {
	names := []string{
		"PORT", "HOST", "DATABASE_URL", "DATA_DIR", "S3_ENDPOINT", "S3_REGION", "S3_BUCKET",
		"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "X-Forwarded-Host", "X-Forwarded-Proto", "/app/start.sh",
		"ygg-store", "us-east-1",
	}
	for _, file := range []string{"prompts/implementer.md", "templates/api/AGENTS.md"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, name := range names {
			if !strings.Contains(text, name) {
				t.Errorf("%s does not name %s", file, name)
			}
		}
		// The presign client bullet says how a URL is signed for the public address.
		if i := strings.Index(text, "presign client"); i < 0 {
			t.Errorf("%s does not describe the presign client", file)
		} else {
			bullet := strings.Join(strings.Fields(text[i:min(i+900, len(text))]), " ")
			for _, want := range []string{"S3_ENDPOINT", "path-style", "UsePathStyle", "S3_REGION", "virtual-host"} {
				if !strings.Contains(bullet, want) {
					t.Errorf("%s: the presign client bullet does not mention %s", file, want)
				}
			}
		}
		if !strings.Contains(text, "r.TLS") {
			t.Errorf("%s does not say what to do when X-Forwarded-Host is absent (the request's Host, https only when r.TLS is set)", file)
		}
		flat := strings.Join(strings.Fields(strings.ToLower(text)), " ")
		if !strings.Contains(flat, "photo bytes and zips of photos never pass through the api") {
			t.Errorf("%s does not say that photo bytes and zips of photos never pass through the API", file)
		}
	}
}

// TestPromptNamesTheRelease: the implementer prompt and the template's AGENTS.md name release.sh and say to
// keep it working.
func TestPromptNamesTheRelease(t *testing.T) {
	for _, file := range []string{"prompts/implementer.md", "templates/api/AGENTS.md"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		flat := strings.Join(strings.Fields(string(raw)), " ")
		if !strings.Contains(flat, "release.sh") {
			t.Errorf("%s does not name release.sh", file)
		}
		if !strings.Contains(flat, "Keep `release.sh` working") && !strings.Contains(flat, "keep `release.sh` working") {
			t.Errorf("%s does not say to keep release.sh working", file)
		}
		if !strings.Contains(flat, "only when the leaf's build changes") {
			t.Errorf("%s does not say to change release.sh only when the leaf's build changes", file)
		}
	}
}

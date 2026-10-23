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
		flat := strings.Join(strings.Fields(strings.ToLower(text)), " ")
		if !strings.Contains(flat, "photo bytes and zips of photos never pass through the api") {
			t.Errorf("%s does not say that photo bytes and zips of photos never pass through the API", file)
		}
	}
}

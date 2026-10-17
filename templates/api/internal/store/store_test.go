package store

import (
	"sort"
	"strings"
	"testing"
)

func TestMigrationsAreNamedInTheOrderTheyRun(t *testing.T) {
	names, err := migrationNames()
	if err != nil || len(names) == 0 {
		t.Fatalf("migrationNames() = %v, %v", names, err)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("migrations %v are not in file order", names)
	}
	for i, n := range names {
		if !strings.HasSuffix(n, ".sql") || len(n) < 6 || strings.Trim(n[:4], "0123456789") != "" {
			t.Errorf("migration %q is not named NNNN_what.sql", n)
		}
		if i > 0 && n[:4] == names[i-1][:4] {
			t.Errorf("migrations %q and %q share a number", names[i-1], n)
		}
	}
}

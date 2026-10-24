package t

import "testing"

func TestAgain(t *testing.T) {
	if A() != 1 {
		t.Fatal("A")
	}
}

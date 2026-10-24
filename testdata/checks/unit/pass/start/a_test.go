package t

import "testing"

func TestA(t *testing.T) {
	if A() != 1 {
		t.Fatal("A")
	}
}

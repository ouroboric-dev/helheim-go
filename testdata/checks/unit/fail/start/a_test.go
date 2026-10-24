package t

import "testing"

func TestA(t *testing.T) {
	if A() != 2 {
		t.Fatal("A")
	}
}

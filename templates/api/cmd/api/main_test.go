package main

import "testing"

func TestPortComesFromTheEnvironmentOr8080(t *testing.T) {
	t.Setenv("PORT", "")
	if got, err := portFromEnv(); err != nil || got != 8080 {
		t.Errorf("portFromEnv() with no PORT = %d, %v; want 8080", got, err)
	}
	t.Setenv("PORT", "9090")
	if got, err := portFromEnv(); err != nil || got != 9090 {
		t.Errorf("portFromEnv() = %d, %v; want 9090", got, err)
	}
	for _, bad := range []string{"0", "65536", "-1", "http", "80 80", "8080;"} {
		t.Setenv("PORT", bad)
		if got, err := portFromEnv(); err == nil {
			t.Errorf("portFromEnv() with PORT=%q = %d, nil; want an error", bad, got)
		}
	}
}

func TestListensOnHost(t *testing.T) {
	t.Setenv("HOST", "127.0.0.1")
	if got := listenAddr(8080); got != "127.0.0.1:8080" {
		t.Errorf("listenAddr(8080) with HOST=127.0.0.1 = %q; want 127.0.0.1:8080", got)
	}
	t.Setenv("HOST", "")
	if got := listenAddr(8080); got != ":8080" {
		t.Errorf("listenAddr(8080) with no HOST = %q; want :8080 (all interfaces)", got)
	}
}

// Package authztest is the two-tenant check of the API: user B asks for a resource of user A and must not get
// it. Every route of openapi.yaml with a variable in its path (each is a resource id) has a test named TestCrossTenant<OperationId>
// that calls Check (the factory's rung go-authz.sh fails without one). Check does not trust a middleware: it
// sends real requests to the handler the server runs.
package authztest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
)

// The two users. Request builds a request for the one it is given, in whatever way the server knows a user.
const (
	UserA = "user-a"
	UserB = "user-b"
)

// T is the part of *testing.T that Check uses.
type T interface {
	Helper()
	Name() string
	Errorf(format string, args ...any)
}

// Case is one route. Operation is its operationId in openapi.yaml. Request builds a request for the resource
// of UserA, signed in as the user it is given.
type Case struct {
	Operation string
	Request   func(user string) *http.Request
}

// Check sends the request as UserA and as UserB. UserA must get a 2xx, so that the request names a resource
// that is there; UserB must get 403 or 404, and nothing else (a 401 says the request was not signed in, a 5xx
// says it failed). The calling test must be named TestCrossTenant<Operation>, with the first letter of the
// operationId in capitals. It also writes both answers to Out, for the factory to read.
func Check(t T, h http.Handler, c Case) {
	t.Helper()
	if c.Operation == "" || c.Request == nil {
		t.Errorf("a case needs an Operation and a Request")
		return
	}
	want := "TestCrossTenant" + strings.ToUpper(c.Operation[:1]) + c.Operation[1:]
	if top, _, _ := strings.Cut(t.Name(), "/"); top != want {
		t.Errorf("the test of operation %s must be named %s, it is %s", c.Operation, want, top)
		return
	}
	a := send(h, c.Request(UserA))
	b := send(h, c.Request(UserB))
	record(t, c.Operation, want, a, b)
	if a < 200 || a > 299 {
		t.Errorf("%s: user A got %d for a resource of user A, want 2xx; the request must name a resource that is there", c.Operation, a)
	}
	if b != http.StatusForbidden && b != http.StatusNotFound {
		t.Errorf("%s: user B got %d for a resource of user A, want 403 or 404", c.Operation, b)
	}
}

func send(h http.Handler, r *http.Request) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

// Out is where Check writes what both users got, as one line that starts with "YGG-AUTHZ ". `go test -json`
// carries it to the factory's rung go-authz.sh. A test of this package may point it elsewhere.
var Out io.Writer = os.Stdout

func record(t T, operation, test string, a, b int) {
	line, err := json.Marshal(struct {
		Operation string `json:"operation"`
		Test      string `json:"test"`
		A         int    `json:"a"`
		B         int    `json:"b"`
	}{operation, test, a, b})
	if err != nil {
		t.Errorf("authztest: %v", err)
		return
	}
	if _, err := fmt.Fprintf(Out, "YGG-AUTHZ %s\n", line); err != nil {
		t.Errorf("authztest: %v", err)
	}
}

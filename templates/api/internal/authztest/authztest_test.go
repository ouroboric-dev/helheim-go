package authztest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeT is a test the checks can fail without failing this one.
type fakeT struct {
	name   string
	failed []string
}

func (f *fakeT) Helper()      {}
func (f *fakeT) Name() string { return f.name }
func (f *fakeT) Errorf(format string, args ...any) {
	f.failed = append(f.failed, fmt.Sprintf(format, args...))
}

// notes serves GET /notes/{id}: the note n1 belongs to user-a. other is what any other user gets.
func notes(other int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User") == UserA {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(other)
	})
}

func get(user string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/notes/n1", nil)
	r.Header.Set("X-User", user)
	return r
}

func TestAnOwnerSeesTheNoteAndAnotherUserDoesNot(t *testing.T) {
	for _, other := range []int{http.StatusForbidden, http.StatusNotFound} {
		ft := &fakeT{name: "TestCrossTenantGetNote"}
		Check(ft, notes(other), Case{Operation: "getNote", Request: get})
		if len(ft.failed) != 0 {
			t.Errorf("user B got %d: %v", other, ft.failed)
		}
	}
}

func TestACheckFailsWhenAnotherUserGetsAnythingButForbiddenOrNotFound(t *testing.T) {
	for _, other := range []int{http.StatusOK, http.StatusNoContent, http.StatusInternalServerError, http.StatusUnauthorized, http.StatusBadRequest} {
		ft := &fakeT{name: "TestCrossTenantGetNote"}
		Check(ft, notes(other), Case{Operation: "getNote", Request: get})
		if len(ft.failed) == 0 || !strings.Contains(ft.failed[0], "user B") {
			t.Errorf("user B got %d and the check passed: %v", other, ft.failed)
		}
	}
}

func TestACheckFailsWhenTheOwnerCannotSeeTheResource(t *testing.T) {
	ft := &fakeT{name: "TestCrossTenantGetNote"}
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	Check(ft, h, Case{Operation: "getNote", Request: get})
	if len(ft.failed) == 0 || !strings.Contains(ft.failed[0], "user A") {
		t.Errorf("user A got 404 and the check passed: %v", ft.failed)
	}
}

func TestACheckFailsWhenTheTestIsNotNamedForTheOperation(t *testing.T) {
	for _, name := range []string{"TestGetNote", "TestCrossTenantGetNotes", "TestCrossTenantgetNote"} {
		ft := &fakeT{name: name}
		Check(ft, notes(http.StatusNotFound), Case{Operation: "getNote", Request: get})
		if len(ft.failed) == 0 || !strings.Contains(ft.failed[0], "TestCrossTenantGetNote") {
			t.Errorf("a test named %s passed: %v", name, ft.failed)
		}
	}
	ft := &fakeT{name: "TestCrossTenantGetNote/subtest"}
	Check(ft, notes(http.StatusNotFound), Case{Operation: "getNote", Request: get})
	if len(ft.failed) != 0 {
		t.Errorf("a subtest of the right test failed: %v", ft.failed)
	}
}

func TestACheckWritesWhatBothUsersGotForTheFactoryToRead(t *testing.T) {
	var out strings.Builder
	defer func(old io.Writer) { Out = old }(Out)
	Out = &out
	ft := &fakeT{name: "TestCrossTenantGetNote"}
	Check(ft, notes(http.StatusNotFound), Case{Operation: "getNote", Request: get})
	want := `YGG-AUTHZ {"operation":"getNote","test":"TestCrossTenantGetNote","a":200,"b":404}` + "\n"
	if out.String() != want {
		t.Errorf("Out = %q; want %q", out.String(), want)
	}
}

package photos

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/authztest"
)

func TestCrossTenantGetPhoto(t *testing.T) {
	authztest.Check(t, Handler(), authztest.Case{Operation: "getPhoto", Request: func(user string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/photos/p1", nil)
		r.Header.Set("X-User", user)
		return r
	}})
}

package helheim

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// go-authz.sh is the two-tenant authorisation harness (spec §9.5 rung 4, v0-T260): every route of openapi.yaml
// that takes a resource id has a test TestCrossTenant<OperationId> that passes, in which user B asks for user
// A's resource and gets 403 or 404 while user A gets it. These tests run it on small leaves, with the real go and
// the real ygg-authz (built here from devcontainer/checks/lib/authz, as the Containerfile builds it).

var (
	authzOnce sync.Once
	authzBin  string
	authzErr  error
)

// authzTool builds ygg-authz once and returns the folder that holds it. The build needs the module proxy
// the first time; the test skips when it is not there, unless YGG_REQUIRE_TEMPLATES is set.
func authzTool(t *testing.T) string {
	t.Helper()
	authzOnce.Do(func() {
		authzBin, authzErr = os.MkdirTemp("", "ygg-authz")
		if authzErr != nil {
			return
		}
		src, _ := filepath.Abs(filepath.Join("devcontainer", "checks", "lib", "authz"))
		cmd := exec.Command("go", "build", "-o", filepath.Join(authzBin, "ygg-authz"), ".")
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
		if out, err := cmd.CombinedOutput(); err != nil {
			authzErr = &buildError{err, string(out)}
		}
	})
	if authzErr != nil {
		if os.Getenv("YGG_REQUIRE_TEMPLATES") != "" {
			t.Fatal(authzErr)
		}
		t.Skipf("ygg-authz can't be built (set YGG_REQUIRE_TEMPLATES to fail instead): %v", authzErr)
	}
	return authzBin
}

type buildError struct {
	err error
	out string
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

const photosAPI = `openapi: 3.1.0
info: {title: API, version: 0.1.0}
paths:
  /healthz:
    get:
      operationId: getHealth
      responses: {"200": {description: ok}}
  /photos/{photoId}:
    get:
      operationId: getPhoto
      responses: {"200": {description: ok}}
`

const photosCode = `package photos

import "net/http"

// Handler serves GET /photos/{photoId}. The photo p1 is user-a's. leak makes the handler answer anyone.
func Handler(leak bool, other int) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /photos/{photoId}", func(w http.ResponseWriter, r *http.Request) {
		if leak || r.Header.Get("X-User") == "user-a" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(other)
	})
	return mux
}
`

// crossTenant is the test a leaf writes for an operation, with a handler it is given.
func crossTenant(op, handler string) string {
	return `package photos

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/authztest"
)

func TestCrossTenant` + strings.ToUpper(op[:1]) + op[1:] + `(t *testing.T) {
	authztest.Check(t, ` + handler + `, authztest.Case{Operation: "` + op + `", Request: func(user string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/photos/p1", nil)
		r.Header.Set("X-User", user)
		return r
	}})
}
`
}

// authzLeaf is a leaf with the authztest package of the template, the photos handler and the given extra files.
func authzLeaf(t *testing.T, files map[string]string) string {
	t.Helper()
	all := map[string]string{
		"go.mod":                      "module api\n\ngo 1.26\n",
		"openapi.yaml":                photosAPI,
		"internal/photos/photos.go":   photosCode,
		"internal/authztest/check.go": mustRead(t, filepath.Join("templates", "api", "internal", "authztest", "authztest.go")),
	}
	for k, v := range files {
		all[k] = v
	}
	return leaf(t, all)
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runAuthz(t *testing.T, dir string, env ...string) (int, string) {
	t.Helper()
	return script(t, dir, authzTool(t), "go-authz.sh", env...)
}

func TestAuthzPassesWhenEveryRouteWithAResourceIdHasACrossTenantTest(t *testing.T) {
	dir := authzLeaf(t, map[string]string{"internal/photos/photos_test.go": crossTenant("getPhoto", "Handler(false, http.StatusNotFound)")})
	code, out := runAuthz(t, dir)
	if code != 0 || !strings.Contains(out, "covered getPhoto (TestCrossTenantGetPhoto: user A 200, user B 404)") || !strings.Contains(out, "authz: ok") {
		t.Fatalf("a covered route: exit %d\n%s", code, out)
	}
	if strings.Contains(out, "getHealth") {
		t.Errorf("a route with no resource id was asked for a test:\n%s", out)
	}
	dir = authzLeaf(t, map[string]string{"internal/photos/photos_test.go": crossTenant("getPhoto", "Handler(false, http.StatusForbidden)")})
	if code, out := runAuthz(t, dir); code != 0 || !strings.Contains(out, "user B 403") {
		t.Errorf("a 403: exit %d\n%s", code, out)
	}
}

func TestAuthzFailsWhenARouteWithAResourceIdHasNoTest(t *testing.T) {
	dir := authzLeaf(t, nil)
	code, out := runAuthz(t, dir)
	if code == 0 || !strings.Contains(out, "missing getPhoto GET /photos/{photoId}: no passing test TestCrossTenantGetPhoto") || !strings.Contains(out, "authz: 1 problem(s)") {
		t.Errorf("a route with no test: exit %d\n%s", code, out)
	}
	// A test for some other route does not cover this one.
	other := strings.ReplaceAll(crossTenant("getPhoto", "Handler(false, http.StatusNotFound)"), "TestCrossTenantGetPhoto", "TestCrossTenantListPhotos")
	if code, out := runAuthz(t, authzLeaf(t, map[string]string{"internal/photos/photos_test.go": other})); code == 0 || !strings.Contains(out, "no passing test TestCrossTenantGetPhoto") {
		t.Errorf("a test of another name: exit %d\n%s", code, out)
	}
}

func TestAuthzFailsWhenUserBGetsTheResource(t *testing.T) {
	for name, handler := range map[string]string{
		"a leak":          "Handler(true, http.StatusNotFound)",
		"a server error":  "Handler(false, http.StatusInternalServerError)",
		"an unauthorised": "Handler(false, http.StatusUnauthorized)",
	} {
		dir := authzLeaf(t, map[string]string{"internal/photos/photos_test.go": crossTenant("getPhoto", handler)})
		if code, out := runAuthz(t, dir); code == 0 || !strings.Contains(out, "getPhoto") {
			t.Errorf("%s: exit %d\n%s", name, code, out)
		}
	}
}

func TestAuthzFailsOnATestThatNamesTheRightNameAndChecksNothing(t *testing.T) {
	cases := map[string]string{
		"an empty test": "package photos\n\nimport \"testing\"\n\nfunc TestCrossTenantGetPhoto(t *testing.T) {}\n",
		"a skipped one": "package photos\n\nimport \"testing\"\n\nfunc TestCrossTenantGetPhoto(t *testing.T) { t.Skip(\"later\") }\n",
		"a check for another operation": strings.Replace(crossTenant("getPhoto", "Handler(false, http.StatusNotFound)"),
			`Operation: "getPhoto"`, `Operation: "listPhotos"`, 1),
	}
	for name, body := range cases {
		dir := authzLeaf(t, map[string]string{"internal/photos/photos_test.go": body})
		code, out := runAuthz(t, dir)
		if code == 0 || !strings.Contains(out, "missing getPhoto") {
			t.Errorf("%s: exit %d\n%s", name, code, out)
		}
	}
}

func TestAuthzFailsWhenTheLeafDoesNotCompile(t *testing.T) {
	dir := authzLeaf(t, map[string]string{"internal/photos/photos_test.go": "package photos\n\nfunc TestCrossTenantGetPhoto(t *testing.T) {\n"})
	if code, out := runAuthz(t, dir); code == 0 || !strings.Contains(out, "missing getPhoto") {
		t.Errorf("a leaf that does not compile: exit %d\n%s", code, out)
	}
}

func TestAuthzReadsTheRoutesWhateverTheShapeOfTheYAML(t *testing.T) {
	spec := `openapi: 3.1.0
info: {title: API, version: 0.1.0}
paths: {
  "/a/{id}": {get: {operationId: getA}, delete: {operationId: deleteA}},
  "/b/{galleryId}/items/{item_id}": {put: {operationId: putItem}},
  "/c/{token}": {get: {operationId: getByToken}},
  "/d/{photoID}":
    {post: {operationId: postD}},
  "/e": {get: {operationId: listE}}}
`
	dir := authzLeaf(t, map[string]string{"openapi.yaml": spec})
	_, out := runAuthz(t, dir)
	for _, want := range []string{"require getA GET /a/{id} TestCrossTenantGetA", "require deleteA DELETE /a/{id} TestCrossTenantDeleteA",
		"require putItem PUT /b/{galleryId}/items/{item_id} TestCrossTenantPutItem", "require postD POST /d/{photoID} TestCrossTenantPostD"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"getByToken", "listE"} {
		if strings.Contains(out, "require "+not) {
			t.Errorf("a route with no resource id is required to have a test: %s\n%s", not, out)
		}
	}
}

func TestAuthzRefusesASpecItCannotReadWholly(t *testing.T) {
	cases := map[string]struct{ spec, want string }{
		"no operationId":     {"openapi: 3.1.0\npaths:\n  /a/{id}:\n    get:\n      responses: {}\n", "GET /a/{id} has no operationId"},
		"a duplicate id":     {"openapi: 3.1.0\npaths:\n  /a/{id}:\n    get: {operationId: x}\n    put: {operationId: x}\n", "operationId x is used twice"},
		"a bad operation id": {"openapi: 3.1.0\npaths:\n  /a/{id}:\n    get: {operationId: get-a}\n", "operationId get-a"},
		"a path item ref":    {"openapi: 3.1.0\npaths:\n  /a/{id}:\n    $ref: other.yaml\n", "/a/{id} is a $ref"},
		"no paths":           {"openapi: 3.1.0\n", "has no paths"},
		"not yaml":           {"paths: [\n", "openapi.yaml"},
	}
	for name, c := range cases {
		code, out := runAuthz(t, authzLeaf(t, map[string]string{"openapi.yaml": c.spec}))
		if code == 0 || !strings.Contains(out, c.want) {
			t.Errorf("%s: exit %d, want %q in\n%s", name, code, c.want, out)
		}
	}
	// A leaf with no contract has nothing to check against: the contract rung says so, and so does this one.
	dir := leaf(t, map[string]string{"go.mod": goMod})
	if code, out := runAuthz(t, dir); code == 0 || !strings.Contains(out, "no openapi.yaml") {
		t.Errorf("no openapi.yaml: exit %d\n%s", code, out)
	}
}

func TestAuthzListsAPublicRouteFromTheRegistryAndRefusesAStaleOne(t *testing.T) {
	spec := photosAPI + "  /shares/{shareId}:\n    get:\n      operationId: getShare\n      responses: {\"200\": {description: ok}}\n"
	files := map[string]string{
		"openapi.yaml":                   spec,
		"internal/photos/photos_test.go": crossTenant("getPhoto", "Handler(false, http.StatusNotFound)"),
		"authz.json":                     `{"schema":"ygg.authz/v1","public":{"getShare":"a share link is the capability: whoever holds it may read"}}`,
	}
	code, out := runAuthz(t, authzLeaf(t, files))
	if code != 0 || !strings.Contains(out, "public getShare: a share link is the capability") {
		t.Errorf("a public route: exit %d\n%s", code, out)
	}
	for name, reg := range map[string]string{
		"a route that is not there":    `{"schema":"ygg.authz/v1","public":{"getShare":"x y","getNothing":"gone"}}`,
		"a route that is not resource": `{"schema":"ygg.authz/v1","public":{"getShare":"x y","getHealth":"no id"}}`,
		"no reason":                    `{"schema":"ygg.authz/v1","public":{"getShare":""}}`,
		"another schema":               `{"schema":"ygg.authz/v2","public":{}}`,
		"an unknown field":             `{"schema":"ygg.authz/v1","public":{},"skip":["getPhoto"]}`,
	} {
		files["authz.json"] = reg
		if code, out := runAuthz(t, authzLeaf(t, files)); code == 0 || !strings.Contains(out, "authz.json") {
			t.Errorf("%s: exit %d\n%s", name, code, out)
		}
	}
	// The exemption does not cover a route that is not named in it.
	files["authz.json"] = `{"schema":"ygg.authz/v1","public":{"getShare":"a share link is the capability"}}`
	delete(files, "internal/photos/photos_test.go")
	if code, out := runAuthz(t, authzLeaf(t, files)); code == 0 || !strings.Contains(out, "missing getPhoto") {
		t.Errorf("a public route hid another: exit %d\n%s", code, out)
	}
}

func TestAuthzHasNothingToDoForAContractWithoutAResourceId(t *testing.T) {
	spec := "openapi: 3.1.0\ninfo: {title: API, version: 0.1.0}\npaths:\n  /healthz:\n    get:\n      operationId: getHealth\n      responses: {\"200\": {description: ok}}\n"
	dir := authzLeaf(t, map[string]string{"openapi.yaml": spec})
	if code, out := runAuthz(t, dir); code != 0 || !strings.Contains(out, "no route takes a resource id") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestAuthzPassesTheUnchangedTemplate(t *testing.T) {
	dir := copyTemplate(t, "api")
	needModules(t, dir)
	if code, out := runAuthz(t, dir); code != 0 || !strings.Contains(out, "no route takes a resource id") {
		t.Errorf("the template: exit %d\n%s", code, out)
	}
}

func TestAuthzRefusesATamperedModuleCache(t *testing.T) {
	proxyURL := proxyModule(t, "example.invalid/dep", "v1.0.0", "package dep\n\nfunc D() int { return 1 }\n")
	cache := t.TempDir()
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", cache).Run() })
	env := []string{"GOMODCACHE=" + cache, "GOPROXY=" + proxyURL, "GOSUMDB=off", "GONOSUMDB=*"}
	dir := authzLeaf(t, map[string]string{
		"go.mod":                         "module api\n\ngo 1.26\n\nrequire example.invalid/dep v1.0.0\n",
		"internal/photos/photos_test.go": crossTenant("getPhoto", "Handler(false, http.StatusNotFound)"),
		"dep.go":                         "package api\n\nimport _ \"example.invalid/dep\"\n",
	})
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = append(os.Environ(), append(env, "GOWORK=off", "GOFLAGS=-mod=mod")...)
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	if _, out := runAuthz(t, dir, env...); strings.Contains(out, "module cache") {
		t.Errorf("an untouched cache was refused:\n%s", out)
	}
	if err := exec.Command("chmod", "-R", "u+w", cache).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "example.invalid", "dep@v1.0.0", "evil.go"), []byte("package dep\n\nimport \"os\"\n\nfunc init() { os.Exit(0) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runAuthz(t, dir, env...); code == 0 || !strings.Contains(out, "module cache") {
		t.Errorf("a dependency edited in the cache: exit %d\n%s", code, out)
	}
}

func TestTheAuthzRungIsInThePackAndTheImageBuildsItsToolFromPinnedSource(t *testing.T) {
	pack, _ := os.ReadFile("pack.yaml")
	if !strings.Contains(string(pack), "authz: [/opt/ygg/checks/go-authz.sh]") || !strings.Contains(string(pack), "security, authz, integration]") {
		t.Error("pack.yaml does not run go-authz.sh as the check authz, after security")
	}
	cf := mustRead(t, filepath.Join("devcontainer", "Containerfile"))
	for _, want := range []string{"COPY checks/lib/authz/ /tmp/ygg-authz/", "-mod=readonly", "-o /usr/local/bin/ygg-authz"} {
		if !strings.Contains(cf, want) {
			t.Errorf("the Containerfile lacks %q", want)
		}
	}
	mod := mustRead(t, filepath.Join("devcontainer", "checks", "lib", "authz", "go.mod"))
	sum := mustRead(t, filepath.Join("devcontainer", "checks", "lib", "authz", "go.sum"))
	if !strings.Contains(mod, "go.yaml.in/yaml/v3 v3.0.5") || !strings.Contains(sum, "go.yaml.in/yaml/v3 v3.0.5 h1:") {
		t.Error("the tool's module does not pin go.yaml.in/yaml/v3 with its hash in go.sum")
	}
}

// The rung reads the answers from the output of the leaf's tests, and a test of the leaf can print anything. A
// test that skips authztest.Check and reports what it likes must still be judged by what it reports.
func TestAuthzDoesNotTrustALeafTestThatReportsWhatItLikes(t *testing.T) {
	forged := func(a, b string) string {
		return strings.NewReplacer("AAA", a, "BBB", b).Replace("package photos\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\n" +
			"func TestCrossTenantGetPhoto(t *testing.T) {\n\tfmt.Println(\"YGG-AUTHZ {\\\"operation\\\":\\\"getPhoto\\\",\\\"test\\\":\\\"TestCrossTenantGetPhoto\\\",\\\"a\\\":AAA,\\\"b\\\":BBB}\")\n}\n")
	}
	if code, out := runAuthz(t, authzLeaf(t, map[string]string{"internal/photos/photos_test.go": forged("200", "404")})); code != 0 {
		t.Fatalf("a test that reports user A 200 and user B 404 (the control): exit %d\n%s", code, out)
	}
	for name, body := range map[string]string{
		"user B got the photo":      forged("200", "200"),
		"user B was not signed in":  forged("200", "401"),
		"user B met a server error": forged("200", "500"),
		"user A got nothing":        forged("404", "404"),
		"user A was refused":        forged("403", "404"),
	} {
		code, out := runAuthz(t, authzLeaf(t, map[string]string{"internal/photos/photos_test.go": body}))
		if code == 0 || !strings.Contains(out, "missing getPhoto") {
			t.Errorf("%s: exit %d\n%s", name, code, out)
		}
	}
}

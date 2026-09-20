package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests describe both worlds: a binary built with the dashboard and
// one built without. Which one runs depends on whether `make web` has been
// run, so each asserts only what is true for the build it is in.

func TestHandlerAlwaysAnswers(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, want HTML", rec.Header().Get("Content-Type"))
	}
	if rec.Body.Len() == 0 {
		t.Error("the handler returned an empty body")
	}
}

func TestUnbuiltDashboardSaysSo(t *testing.T) {
	if Available() {
		t.Skip("this binary was built with the dashboard")
	}

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "without the dashboard") {
		t.Errorf("the placeholder should explain itself:\n%s", body)
	}
	if !strings.Contains(body, "make web") {
		t.Error("the placeholder should say how to build it")
	}
	if !strings.Contains(body, "agents work normally") {
		t.Error("the placeholder should make clear the API still works")
	}
}

func TestClientRoutesFallBackToTheShell(t *testing.T) {
	if !Available() {
		t.Skip("the dashboard is not built into this binary")
	}

	// A deep link is the browser router's job, so it must get the shell and
	// a 200 rather than a 404 it cannot route.
	for _, path := range []string{"/runners", "/runners/abc-123", "/settings"} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "<div id=\"root\"") {
			t.Errorf("%s did not return the app shell", path)
		}
	}
}

func TestAssetsAreCachedAndTheShellIsNot(t *testing.T) {
	if !Available() {
		t.Skip("the dashboard is not built into this binary")
	}

	shell := httptest.NewRecorder()
	Handler().ServeHTTP(shell, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if got := shell.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache: a cached shell serves an old build's scripts", got)
	}

	// Find a hashed asset from the shell and check it is cached hard.
	body := shell.Body.String()
	start := strings.Index(body, "/assets/")
	if start < 0 {
		t.Fatalf("no asset reference in the shell:\n%s", body)
	}
	end := strings.IndexAny(body[start:], `"'`)
	if end < 0 {
		t.Fatal("could not find the end of the asset path")
	}
	asset := body[start : start+end]

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d, want 200", asset, rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("%s Cache-Control = %q, want it cached hard", asset, rec.Header().Get("Cache-Control"))
	}
}

func TestPathTraversalCannotEscapeTheBundle(t *testing.T) {
	for _, path := range []string{"/../go.mod", "/assets/../../go.mod", "//etc/passwd"} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

		if strings.Contains(rec.Body.String(), "module github.com/bablilayoub/runnerly") {
			t.Errorf("%s served a file from outside the bundle", path)
		}
	}
}

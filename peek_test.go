package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOnlyRegisteredFilesAreServed(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "page.html")
	if err := os.WriteFile(shared, []byte("<h1>hello</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := LoadRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Add(shared, ""); err != nil {
		t.Fatal(err)
	}
	handler := shareHandler(registry)

	if body, status := get(handler, "/page.html"); status != http.StatusOK || body != "<h1>hello</h1>" {
		t.Errorf("shared file: got %d %q", status, body)
	}
	if _, status := get(handler, "/secret.txt"); status != http.StatusNotFound {
		t.Errorf("unregistered file: got %d, want 404", status)
	}
	if _, status := get(handler, "/../registry.json"); status != http.StatusNotFound {
		t.Errorf("path climbing out: got %d, want 404", status)
	}
}

func TestShareSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "page.html")
	if err := os.WriteFile(file, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	registryFile := filepath.Join(dir, "registry.json")

	first, err := LoadRegistry(registryFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Add(file, "report"); err != nil {
		t.Fatal(err)
	}

	restarted, err := LoadRegistry(registryFile)
	if err != nil {
		t.Fatal(err)
	}
	// The registry stores the file with its symlinks resolved, which on macOS
	// turns /var into /private/var, so the test resolves its expectation too.
	want, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	share, exists := restarted.Lookup("report")
	if !exists || share.Path != want {
		t.Fatalf("after restart: got %+v, exists %v", share, exists)
	}

	if _, err := restarted.Remove("report"); err != nil {
		t.Fatal(err)
	}
	if _, exists := restarted.Lookup("report"); exists {
		t.Error("share still found after rm")
	}
}

func TestAddRejectsWhatCannotBeShared(t *testing.T) {
	dir := t.TempDir()
	registry, err := LoadRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := registry.Add(dir, ""); err == nil {
		t.Error("a directory was accepted")
	}
	if _, err := registry.Add(filepath.Join(dir, "missing.html"), ""); err == nil {
		t.Error("a missing file was accepted")
	}

	file := filepath.Join(dir, "page.html")
	if err := os.WriteFile(file, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Add(file, "a/b"); err == nil {
		t.Error("an alias with a separator was accepted")
	}

	other := filepath.Join(dir, "other.html")
	if err := os.WriteFile(other, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Add(file, "report"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Add(other, "report"); err == nil {
		t.Error("an alias was quietly reassigned to another file")
	}
}

func get(handler http.Handler, path string) (string, int) {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder.Body.String(), recorder.Code
}

func TestIndexShowsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	registry, err := LoadRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}

	for _, share := range []struct {
		alias    string
		modified time.Time
	}{
		{"a-older", time.Now().Add(-2 * time.Hour)},
		{"b-newest", time.Now()},
		{"c-oldest", time.Now().Add(-48 * time.Hour)},
	} {
		file := filepath.Join(dir, share.alias)
		if err := os.WriteFile(file, []byte("body"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, share.modified, share.modified); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Add(file, share.alias); err != nil {
			t.Fatal(err)
		}
	}

	body, status := get(shareHandler(registry), "/")
	if status != http.StatusOK {
		t.Fatalf("index: got %d, want 200", status)
	}
	if order := []int{strings.Index(body, ">b-newest<"), strings.Index(body, ">a-older<"), strings.Index(body, ">c-oldest<")}; !(order[0] < order[1] && order[1] < order[2]) {
		t.Errorf("index is not newest first, positions %v in:\n%s", order, body)
	}
}

func TestListByNewestOrdersByFileTime(t *testing.T) {
	dir := t.TempDir()
	registry, err := LoadRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, share := range []struct {
		alias    string
		modified time.Time
	}{
		{"a-older", time.Now().Add(-2 * time.Hour)},
		{"b-newest", time.Now()},
		{"c-oldest", time.Now().Add(-48 * time.Hour)},
	} {
		file := filepath.Join(dir, share.alias)
		if err := os.WriteFile(file, []byte("body"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, share.modified, share.modified); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Add(file, share.alias); err != nil {
			t.Fatal(err)
		}
	}

	var order []string
	for _, share := range registry.ListByNewest() {
		order = append(order, share.Alias)
	}
	if want := []string{"b-newest", "a-older", "c-oldest"}; !slices.Equal(order, want) {
		t.Errorf("got %v, want %v", order, want)
	}
}

func TestBrowserGetsAWayBackToTheList(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.html")
	if err := os.WriteFile(page, []byte("<h1>hello</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	download := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(download, []byte("plain"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := LoadRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{page, download} {
		if _, err := registry.Add(file, ""); err != nil {
			t.Fatal(err)
		}
	}
	handler := shareHandler(registry)

	body, status := browserGet(handler, "/page.html")
	if status != http.StatusOK || !strings.Contains(body, `href="/"`) {
		t.Errorf("browser opening a page: got %d, no way back in:\n%s", status, body)
	}
	if body, _ := browserGet(handler, "/page.html?raw=1"); body != "<h1>hello</h1>" {
		t.Errorf("framed page: got %q, want the file itself", body)
	}
	if body, _ := browserGet(handler, "/notes.txt"); body != "plain" {
		t.Errorf("a file that is not a page: got %q, want the file itself", body)
	}
	if body, _ := get(handler, "/page.html"); body != "<h1>hello</h1>" {
		t.Errorf("curl: got %q, want the file itself", body)
	}
}

func browserGet(handler http.Handler, path string) (string, int) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Sec-Fetch-Dest", "document")
	handler.ServeHTTP(recorder, request)
	return recorder.Body.String(), recorder.Code
}

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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

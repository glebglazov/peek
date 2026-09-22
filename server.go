package main

import (
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// pageStyle is peek's own chrome — the index and the viewer bar. It never
// reaches the shared file, which is served exactly as it sits on disk.
const pageStyle = `<style>
  /* The page follows the device, phone included. The colour scheme is declared
     in a meta tag rather than here, because the browser reads that before it
     parses any CSS and so paints the first frame in the right colour. */
  :root {
    --page: #ffffff;
    --ink: #1a1a1a;
    --muted: #6a6a6a;
    --link: #0b57d0;
    --edge: #d8d8d8;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --page: #16181c;
      --ink: #e8e6e3;
      --muted: #9aa0a6;
      --link: #8ab4f8;
      --edge: #2c2f34;
    }
  }
  body {
    margin: 0 auto;
    padding: 2rem 1.25rem;
    max-width: 40rem;
    background: var(--page);
    color: var(--ink);
    font: 1rem/1.6 system-ui, -apple-system, sans-serif;
  }
  h1 { font-size: 1.25rem; }
  ul { padding-left: 1.25rem; }
  li { margin-bottom: 0.5rem; }
  a { color: var(--link); }
  small { color: var(--muted); }
</style>`

var indexPage = template.Must(template.New("index").Parse(`<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>peek</title>
` + pageStyle + `
<h1>Shared files</h1>
{{- if .}}
<ul>
{{- range .}}
  <li><a href="/{{.Alias}}">{{.Alias}}</a> <small>{{.Modified}}</small></li>
{{- end}}
</ul>
{{- else}}
<p>Nothing is shared yet.</p>
{{- end}}
`))

// viewerPage puts a way back to the list above a shared page. The file goes in
// a frame rather than being rewritten, so what the reader sees below the bar
// is the file itself, byte for byte, and ?raw still serves it alone.
var viewerPage = template.Must(template.New("viewer").Parse(`<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>{{.Alias}} — peek</title>
` + pageStyle + `<style>
  body { margin: 0; padding: 0; max-width: none; height: 100vh; display: flex; flex-direction: column; }
  header {
    flex: none;
    display: flex;
    gap: 1rem;
    align-items: baseline;
    padding: 0.6rem 1rem;
    border-bottom: 1px solid var(--edge);
  }
  header .name { color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  iframe { flex: 1 1 auto; width: 100%; border: 0; background: var(--page); }
</style>
<header>
  <a href="/">&#8592; All files</a>
  <span class="name">{{.Alias}}</span>
</header>
<iframe src="/{{.Alias}}?raw=1" title="{{.Alias}}"></iframe>
`))

// A listedShare is one line of the index: the alias to follow, and when the
// file behind it last changed, as the reader wants them ordered.
type listedShare struct {
	Alias    string
	Modified string
}

// shareHandler serves the registry and nothing else: a path that names no
// share is a 404, so the rest of the disk stays out of reach.
func shareHandler(registry *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alias := strings.TrimPrefix(r.URL.Path, "/")
		if alias == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			indexPage.Execute(w, listing(registry))
			return
		}

		share, exists := registry.Lookup(alias)
		if !exists {
			http.NotFound(w, r)
			return
		}
		if wantsViewer(r, share) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			viewerPage.Execute(w, share)
			return
		}
		http.ServeFile(w, r, share.Path)
	})
}

// wantsViewer holds the bar back for the one case it helps: a browser opening
// a shared page to read it. The frame asks for ?raw, curl and a download ask
// without the document header at all, and both get the file itself.
func wantsViewer(r *http.Request, share Share) bool {
	if r.Method != http.MethodGet || r.URL.Query().Has("raw") {
		return false
	}
	if r.Header.Get("Sec-Fetch-Dest") != "document" {
		return false
	}
	switch strings.ToLower(filepath.Ext(share.Path)) {
	case ".html", ".htm":
		return true
	}
	return false
}

// listing turns the registry's order into the lines the page shows.
func listing(registry *Registry) []listedShare {
	listed := make([]listedShare, 0)
	for _, share := range registry.ListByNewest() {
		listed = append(listed, listedShare{Alias: share.Alias, Modified: modifiedAt(share.Modified)})
	}
	return listed
}

// A file peek can no longer stat has no time to show, and says so rather than
// claiming the zero date.
func modifiedAt(at time.Time) string {
	if at.IsZero() {
		return "unknown"
	}
	return at.Local().Format("2006-01-02 15:04")
}

func shareURL(baseURL, alias string) string {
	return baseURL + "/" + url.PathEscape(alias)
}

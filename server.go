package main

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

var indexPage = template.Must(template.New("index").Parse(`<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>peek</title>
<style>
  /* The page follows the device, phone included: color-scheme hands the form
     controls and the scrollbar to the browser, the variables cover the rest. */
  :root {
    color-scheme: light dark;
    --page: #ffffff;
    --ink: #1a1a1a;
    --muted: #6a6a6a;
    --link: #0b57d0;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --page: #16181c;
      --ink: #e8e6e3;
      --muted: #9aa0a6;
      --link: #8ab4f8;
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
</style>
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
		http.ServeFile(w, r, share.Path)
	})
}

// listing reads each file's modification time at request time, because the
// registry records what is shared and the disk records when it changed. The
// most recently changed file comes first, which is the one a reader who was
// just sent a link is looking for.
func listing(registry *Registry) []listedShare {
	shares := registry.List()

	modified := make(map[string]time.Time, len(shares))
	for _, share := range shares {
		if info, err := os.Stat(share.Path); err == nil {
			modified[share.Alias] = info.ModTime()
		}
	}

	sort.SliceStable(shares, func(i, j int) bool {
		return modified[shares[i].Alias].After(modified[shares[j].Alias])
	})

	listed := make([]listedShare, 0, len(shares))
	for _, share := range shares {
		listed = append(listed, listedShare{Alias: share.Alias, Modified: modifiedAt(modified[share.Alias])})
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

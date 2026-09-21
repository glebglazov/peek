package main

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

var indexPage = template.Must(template.New("index").Parse(`<!doctype html>
<meta charset="utf-8">
<title>peek</title>
<h1>Shared files</h1>
{{- if .}}
<ul>
{{- range .}}
  <li><a href="/{{.Alias}}">{{.Alias}}</a></li>
{{- end}}
</ul>
{{- else}}
<p>Nothing is shared yet.</p>
{{- end}}
`))

// shareHandler serves the registry and nothing else: a path that names no
// share is a 404, so the rest of the disk stays out of reach.
func shareHandler(registry *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alias := strings.TrimPrefix(r.URL.Path, "/")
		if alias == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			indexPage.Execute(w, registry.List())
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

func shareURL(baseURL, alias string) string {
	return baseURL + "/" + url.PathEscape(alias)
}

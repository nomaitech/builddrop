package main

import (
	"fmt"
	"html/template"
	"net/http"
)

var indexTmpl = template.Must(template.New("index").Funcs(template.FuncMap{
	"size": func(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/1e6) },
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Android builds</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 0 auto; max-width: 40rem; padding: 1rem; }
  .build { border: 1px solid #ccc; border-radius: 8px; padding: .75rem 1rem; margin-bottom: .75rem; }
  .meta { color: #666; font-size: .85rem; }
  .failed { color: #b00020; }
  a.dl { display: inline-block; margin-top: .5rem; padding: .5rem 1rem; background: #1b5e20; color: #fff; border-radius: 6px; text-decoration: none; }
</style>
</head>
<body>
<h1>Android builds</h1>
{{range .}}
<div class="build">
  <strong>{{.Project}}</strong>{{if .VersionName}} v{{.VersionName}} ({{.VersionCode}}){{end}}
  <div class="meta">{{.PackageName}}</div>
  <div class="meta">{{if .Commit}}{{.Branch}} @ {{.Commit}} · {{end}}{{.CreatedAt.Format "2006-01-02 15:04"}} · {{.Task}}</div>
  {{if eq .Status "success"}}<a class="dl" href="/api/builds/{{.ID}}/apk">Download APK ({{size .ApkSize}})</a>
  {{else if eq .Status "failed"}}<div class="failed">Failed: {{.Error}} (<a href="/api/builds/{{.ID}}/log">log</a>)</div>
  {{else}}<div class="meta">{{.Status}}…</div>{{end}}
</div>
{{else}}
<p>No builds yet.</p>
{{end}}
</body>
</html>`))

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexTmpl.Execute(w, s.store.List())
}

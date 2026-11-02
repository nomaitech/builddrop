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
  details { margin-top: .5rem; }
  hr { border: 0; border-top: 1px solid #eee; }
  a.dl { display: inline-block; margin-top: .5rem; padding: .5rem 1rem; background: #1b5e20; color: #fff; border-radius: 6px; text-decoration: none; }
</style>
</head>
<body>
<h1>Android builds</h1>
{{range .}}
<div class="build">
  {{with .Pending}}<div class="meta {{if eq .Status "failed"}}failed{{end}}">Newer build {{.Status}}{{if .Commit}} ({{.Commit}}){{end}}{{if .Error}}: {{.Error}} (<a href="/api/builds/{{.ID}}/log">log</a>){{end}}</div>{{end}}
  {{template "build" .Primary}}
  {{if .Older}}
  <details>
    <summary class="meta">{{len .Older}} older build{{if gt (len .Older) 1}}s{{end}}</summary>
    {{range .Older}}<hr>{{template "build" .}}{{end}}
  </details>
  {{end}}
</div>
{{else}}
<p>No builds yet.</p>
{{end}}
</body>
</html>
{{define "build"}}
  <strong>{{.Project}}</strong>{{if .VersionName}} v{{.VersionName}} ({{.VersionCode}}){{end}}
  <div class="meta">{{.PackageName}}</div>
  <div class="meta">{{if .Commit}}{{.Branch}} @ {{.Commit}} · {{end}}{{.CreatedAt.Format "2006-01-02 15:04"}}{{if ne .Task "assembleDebug"}} · {{.Task}}{{end}}</div>
  {{if eq .Status "success"}}<a class="dl" href="/api/builds/{{.ID}}/apk">Download APK ({{size .ApkSize}})</a>
  {{else if eq .Status "failed"}}<div class="failed">Failed: {{.Error}} (<a href="/api/builds/{{.ID}}/log">log</a>)</div>
  {{else}}<div class="meta">{{.Status}}…</div>{{end}}
{{end}}`))

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexTmpl.Execute(w, groupBuilds(s.store.List()))
}

// BuildGroup holds all builds of one project (repo path).
type BuildGroup struct {
	Primary Build  // newest successful build, or newest build if none succeeded
	Pending *Build // newer build that is still running or failed, if any
	Older   []Build
}

// groupBuilds groups newest-first builds by repo path, keeping that order.
func groupBuilds(builds []Build) []BuildGroup {
	var keys []string
	byRepo := map[string][]Build{}
	for _, b := range builds {
		if _, ok := byRepo[b.RepoPath]; !ok {
			keys = append(keys, b.RepoPath)
		}
		byRepo[b.RepoPath] = append(byRepo[b.RepoPath], b)
	}
	groups := make([]BuildGroup, 0, len(keys))
	for _, k := range keys {
		list := byRepo[k]
		primary := 0
		for i, b := range list {
			if b.Status == StatusSuccess {
				primary = i
				break
			}
		}
		g := BuildGroup{Primary: list[primary]}
		for i, b := range list {
			switch {
			case i == primary:
			case i == 0:
				pending := b
				g.Pending = &pending
			default:
				g.Older = append(g.Older, b)
			}
		}
		groups = append(groups, g)
	}
	return groups
}

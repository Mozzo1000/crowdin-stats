package main

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

type pageData struct {
	Title          string
	ShowGetStarted bool
	LivePreview    bool // true renders the embed-builder's <img> preview (setup), false its inline SVG preview (index)
}

type page struct {
	tmpl *template.Template
	data pageData
}

var pages = map[string]page{
	"index": {
		data: pageData{Title: "crowdin-stats — live translation images for your README", ShowGetStarted: true},
	},
	"setup": {
		data: pageData{Title: "Generate your images — crowdin-stats", ShowGetStarted: false, LivePreview: true},
	},
	"privacy": {
		data: pageData{Title: "Privacy Policy — crowdin-stats", ShowGetStarted: true},
	},
	"terms": {
		data: pageData{Title: "Terms of Service — crowdin-stats", ShowGetStarted: true},
	},
}

func init() {
	for name, p := range pages {
		tmpl, err := template.ParseFS(templateFS, "templates/layout.html", "templates/embed-builder.html", "templates/"+name+".html")
		if err != nil {
			panic("parse template " + name + ": " + err.Error())
		}
		p.tmpl = tmpl
		pages[name] = p
	}
}

// servePage renders a shared-layout page by name (see the `pages` map). It's
// used for the small set of mostly-static routes (/, /setup, /terms,
// /privacy) that used to be served as standalone HTML files with copy-pasted
// nav/footer markup.
func servePage(name string) http.HandlerFunc {
	p := pages[name]
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := p.tmpl.ExecuteTemplate(w, "layout", p.data); err != nil {
			slog.Error("render page failed", "page", name, "error", err)
		}
	}
}

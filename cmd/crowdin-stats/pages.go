package main

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

// siteURL is the canonical base URL (matches Caddyfile's {$HOST}), used to
// build absolute og:url / og:image values — social-media crawlers won't
// resolve relative ones.
const siteURL = "https://crowdin-stats.rewake.org"

type pageData struct {
	Title          string
	Description    string
	Path           string // used to build the absolute og:url
	ShowGetStarted bool
	LivePreview    bool // true shows the embed-builder's snippet-format controls (setup); both its <img> and inline SVG preview elements always render — JS picks between them per embed type
}

// CanonicalURL and OGImageURL are computed (rather than built in the
// template) so siteURL only needs to change in one place.
func (d pageData) CanonicalURL() string { return siteURL + d.Path }
func (d pageData) OGImageURL() string   { return siteURL + "/static/og-image.png" }

type page struct {
	tmpl *template.Template
	data pageData
}

var pages = map[string]page{
	"index": {
		data: pageData{
			Title:          "Crowdin Stats — live translation images for your README",
			Description:    "Generate live, embeddable SVG images showing Crowdin translation progress and top contributors, without exposing your Crowdin token.",
			Path:           "/",
			ShowGetStarted: true,
		},
	},
	"setup": {
		data: pageData{
			Title:          "Generate your images — Crowdin Stats",
			Description:    "Connect a Crowdin project and get back embed URLs for a translation progress table and contributor grid. Takes under a minute, no account required.",
			Path:           "/setup",
			ShowGetStarted: false,
			LivePreview:    true,
		},
	},
	"privacy": {
		data: pageData{
			Title:          "Privacy Policy — Crowdin Stats",
			Description:    "What crowdin-stats stores when you register a project, and how your Crowdin token is encrypted.",
			Path:           "/privacy",
			ShowGetStarted: true,
		},
	},
	"terms": {
		data: pageData{
			Title:          "Terms of Service — Crowdin Stats",
			Description:    "Terms governing use of crowdin-stats' free, self-hosted translation-image embeds.",
			Path:           "/terms",
			ShowGetStarted: true,
		},
	},
	"revoke": {
		data: pageData{
			Title:          "Revoke access — Crowdin Stats",
			Description:    "Revoke a crowdin-stats embed and remove its stored project data.",
			Path:           "/revoke",
			ShowGetStarted: false,
		},
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

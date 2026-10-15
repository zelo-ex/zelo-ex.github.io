package main

import (
	"encoding/json"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// buildAboutPage renders a standalone about page. Content is raw HTML
// (already rendered by the org renderer); everything else is escaped.
func buildAboutPage(about map[string]string) string {
	page := `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8"/>
<title>` + html.EscapeString(about["title"]) + ` - zal-blog</title>
<link href="/style.css" rel="stylesheet"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
</head>
<body>
<div class="term-frame">
<span class="blog-title">` + html.EscapeString(about["title"]) + `</span>
<hr class="splitter" />
<a href="/">back index</a>
<a href="/blog.html">visit blog</a>
<a href="/rss.xml">rss</a>
<div class="content">` + about["html"] + `</div>
</div>
<script src="/js/post.js"></script>
</body>
</html>
`
	return page
}

// buildPostPage renders a standalone post page. Content is raw HTML
// (already rendered by the org renderer); everything else is escaped.
func buildPostPage(meta PostMeta, content string, prev, next *postSummary) string {
	page := `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8"/>
<title>` + html.EscapeString(meta.Title) + ` - zal-blog</title>
<link href="/style.css" rel="stylesheet"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
</head>
<body>
<div class="term-frame">
<span class="blog-title">` + html.EscapeString(meta.Title) + `</span>
<hr class="splitter" />
<a href="/">back index</a>
<a href="/blog.html">visit blog</a>
<a href="/rss.xml">rss</a>
<div class="content">` + content + `</div>`
	if nav := postNavHTML(meta, prev, next); nav != "" {
		page += `
<div class="content">` + nav + `</div>`
	}
	page += `
</div>
<script src="/js/post.js"></script>
</body>
</html>
`
	return page
}

// postNavHTML builds the tags + navigation block. Series chapters get a
// back-to-series link plus previous/next chapter links; standalone posts
// keep the older/newer date navigation.
func postNavHTML(meta PostMeta, prev, next *postSummary) string {
	var lines []string
	if meta.Series != "" {
		title := meta.SeriesTitle
		if title == "" {
			title = meta.Series
		}
		lines = append(lines, `series: <a href="/series/`+url.PathEscape(meta.Series)+`.html">`+html.EscapeString(title)+`</a>`)
	}
	if len(meta.Tags) > 0 {
		parts := make([]string, 0, len(meta.Tags))
		for _, t := range meta.Tags {
			parts = append(parts, `<a href="/tags.html?t=`+url.QueryEscape(t)+`">#`+html.EscapeString(t)+`</a>`)
		}
		lines = append(lines, "tags: "+strings.Join(parts, " "))
	}
	if meta.Series != "" {
		if prev != nil {
			lines = append(lines, `← <a href="/posts/`+prev.Path+`.html">`+html.EscapeString(prev.Title)+`</a>`)
		}
		if next != nil {
			lines = append(lines, `<a href="/posts/`+next.Path+`.html">`+html.EscapeString(next.Title)+`</a> →`)
		}
	} else {
		if prev != nil {
			lines = append(lines, `<a href="/posts/`+prev.Path+`.html">`+html.EscapeString(prev.Title)+` <- next</a>`)
		}
		if next != nil {
			lines = append(lines, `<a href="/posts/`+next.Path+`.html">prev -> `+html.EscapeString(next.Title)+`</a>`)
		}
	}
	return strings.Join(lines, "\n")
}

// handlePostPage dynamically renders /posts/{slug}.html in serve mode so
// file edits show up without running the generator.
func (s *server) handlePostPage(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/posts/")
	p = strings.TrimSuffix(p, ".html")
	if p == "" || !postSlugRe.MatchString(p) {
		s.notFound(w, r)
		return
	}
	meta, content, err := s.loadPost(p)
	if err != nil {
		s.notFound(w, r)
		return
	}
	posts, err := s.scanPosts()
	if err != nil {
		s.notFound(w, r)
		return
	}
	prev, next := findNav(posts, p)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(buildPostPage(meta, content, prev, next))); err != nil {
		log.Printf("handlePostPage: write: %v", err)
	}
}

// ---------------------------------------------------------------------------
// static site generation
// ---------------------------------------------------------------------------

// generate builds the static site into frontend/:
//
//	frontend/posts/{YYYY}/{MM}/{slug}.html   per-post pages
//	frontend/posts.json                       post list
//	frontend/tags.json                        tag counts
//	frontend/about.json                       rendered about page
//	frontend/rss.xml                          RSS feed
//	frontend/assets/                          copied from blog/assets/
//
// Drafts are excluded from everything (they remain previewable only via
// the dev server).
func (s *server) generate(base string) error {
	posts, err := s.scanPosts()
	if err != nil {
		return err
	}
	series, err := s.scanSeries(posts)
	if err != nil {
		return err
	}
	attachSeriesTitles(posts, series)

	postsDir := filepath.Join(s.frontendRoot, "posts")
	if err := os.RemoveAll(postsDir); err != nil {
		return err
	}
	if err := os.MkdirAll(postsDir, 0o755); err != nil {
		return err
	}

	for _, p := range posts {
		_, content, err := s.loadPost(p.Path)
		if err != nil {
			log.Printf("generate: skip %s: %v", p.Path, err)
			continue
		}
		prev, next := findNav(posts, p.Path)
		page := buildPostPage(p, content, prev, next)
		out := filepath.Join(postsDir, filepath.FromSlash(p.Path+".html"))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, []byte(page), 0o644); err != nil {
			return err
		}
	}

	seriesDir := filepath.Join(s.frontendRoot, "series")
	if err := os.RemoveAll(seriesDir); err != nil {
		return err
	}
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		return err
	}
	for _, sm := range series {
		base := filepath.Join(seriesDir, filepath.FromSlash(sm.Slug))
		if err := writeFileJSON(base+".json", sm); err != nil {
			return err
		}
		if err := os.WriteFile(base+".html", []byte(seriesPageShell()), 0o644); err != nil {
			return err
		}
	}

	if err := writeFileJSON(filepath.Join(s.frontendRoot, "posts.json"), map[string]any{"posts": buildFeed(posts, series)}); err != nil {
		return err
	}
	if err := writeFileJSON(filepath.Join(s.frontendRoot, "tags.json"), map[string]any{"tags": buildTags(posts, series)}); err != nil {
		return err
	}
	about, err := s.buildAbout()
	if err != nil {
		return err
	}
	page := buildAboutPage(about)
	if err := os.WriteFile(filepath.Join(s.frontendRoot, "about.html"), []byte(page), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.frontendRoot, "rss.xml"), s.buildRSS(base), 0o644); err != nil {
		return err
	}
	return copyAssets(filepath.Join(s.blogRoot, "assets"), filepath.Join(s.frontendRoot, "assets"))
}

func writeFileJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// copyAssets mirrors blog/assets/ into frontend/assets/.
func copyAssets(srcRoot, dstRoot string) error {
	if _, err := os.Stat(srcRoot); err != nil {
		if os.IsNotExist(err) {
			return nil // no assets yet
		}
		return err
	}
	if err := os.RemoveAll(dstRoot); err != nil {
		return err
	}
	return filepath.WalkDir(srcRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()
		_, err = io.Copy(out, in)
		return err
	})
}

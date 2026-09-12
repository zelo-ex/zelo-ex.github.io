package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultPort = "54345"

func main() {
	blogRoot := resolveDir(getenv("BLOG_DIR", "blog"))
	frontendRoot := resolveDir(getenv("FRONTEND_DIR", "frontend"))
	port := getenv("PORT", defaultPort)

	s := &server{blogRoot: blogRoot, frontendRoot: frontendRoot}

	cmd := "generate" // default: build static site into frontend/
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		cmd = "serve"
	}
	switch cmd {
	case "serve":
		s.serve(port)
	default:
		base := getenv("SITE_BASE", "https://zelo-ex.github.io")
		if len(os.Args) > 1 && os.Args[1] == "generate" && len(os.Args) > 2 {
			base = os.Args[2]
		}
		if err := s.generate(base); err != nil {
			log.Fatal(err)
		}
		log.Printf("generated static site into %s (base=%s)", frontendRoot, base)
	}
}

func (s *server) serve(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/posts", s.handlePosts)
	mux.HandleFunc("GET /api/post", s.handlePost)
	mux.HandleFunc("GET /api/tags", s.handleTags)
	mux.HandleFunc("GET /api/about", s.handleAbout)
	mux.HandleFunc("GET /posts.json", s.handlePosts)
	mux.HandleFunc("GET /tags.json", s.handleTags)
	mux.HandleFunc("GET /about.json", s.handleAbout)
	mux.HandleFunc("GET /rss.xml", s.handleRSS)
	mux.HandleFunc("/", s.handleStatic)

	handler := noStore(logRequests(mux))
	log.Printf("zal-blog listening on http://localhost:%s (blog=%s frontend=%s)", port, s.blogRoot, s.frontendRoot)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	blogRoot     string
	frontendRoot string
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// resolveDir allows running the binary both from the repo root
// (go run ./backend) and from inside backend/ (cd backend && go run .).
// Symlinks are resolved to their real path: filepath.WalkDir does not
// traverse a symlinked root, which would break post scanning.
func resolveDir(name string) string {
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		return resolved
	}
	if alt := filepath.Join("..", name); alt != "" {
		if resolved, err := filepath.EvalSymlinks(alt); err == nil {
			return resolved
		}
	}
	return name
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no caching at all: edits to org/posts/frontend show up immediately
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s", r.Method, r.URL.Path)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ---------------------------------------------------------------------------
// post scanning
// ---------------------------------------------------------------------------

var (
	postPathRe      = regexp.MustCompile(`^\d{4}/\d{2}/[A-Za-z0-9._-]+\.org$`)
	yearPartRe      = regexp.MustCompile(`^\d{4}$`)
	seriesChapterRe = regexp.MustCompile(`^series/([A-Za-z0-9._-]+)/(\d+)-([A-Za-z0-9._-]+)\.org$`)
	seriesSlugRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// classifyPost turns a slash-separated org path (relative to blog root)
// into post metadata, or reports it as a non-post (about.org, series
// index.org, ...).
func classifyPost(rel string) (PostMeta, bool) {
	if postPathRe.MatchString(rel) {
		return PostMeta{Path: strings.TrimSuffix(rel, ".org")}, true
	}
	if m := seriesChapterRe.FindStringSubmatch(rel); m != nil {
		order, _ := strconv.Atoi(m[2])
		return PostMeta{
			Path:        strings.TrimSuffix(rel, ".org"), // keep the NN- prefix in the slug/URL
			Series:      m[1],
			SeriesOrder: order,
		}, true
	}
	return PostMeta{}, false
}

// scanPosts walks blogRoot for published posts (YYYY/MM/name.org and
// series/SLUG/NN-name.org). Drafts (*.sec.org) are excluded.
func (s *server) scanPosts() ([]PostMeta, error) {
	var posts []PostMeta
	err := filepath.WalkDir(s.blogRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != s.blogRoot {
				name := d.Name()
				if name == "assets" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(s.blogRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(rel)
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
			return nil
		}
		if strings.HasSuffix(base, ".sec.org") {
			return nil
		}
		meta, ok := classifyPost(rel)
		if !ok {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		parsed := parseMeta(string(src))
		meta.Title, meta.Date, meta.Tags = parsed.Title, parsed.Date, parsed.Tags
		meta.fillFallbacks()
		if meta.Title == "" {
			meta.Title = fallbackTitle(string(src), meta.Path)
		}
		posts = append(posts, meta)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortPosts(posts)
	return posts, nil
}

// SeriesMeta is a single series, backed by series/SLUG/index.org plus its
// chapters. Start/Latest come from the chapters' own #+DATE values.
type SeriesMeta struct {
	Slug   string        `json:"slug"`
	Title  string        `json:"title"`
	Tags   []string      `json:"tags,omitempty"`
	Start  string        `json:"start,omitempty"`  // earliest chapter date
	Latest string        `json:"latest,omitempty"` // latest chapter date
	Count  int           `json:"count"`
	Intro  string        `json:"intro,omitempty"` // rendered index.org body
	Posts  []postSummary `json:"posts"`
}

// scanSeries builds the series list from blog/series/*/index.org plus the
// chapters gathered by scanPosts.
func (s *server) scanSeries(posts []PostMeta) ([]SeriesMeta, error) {
	root := filepath.Join(s.blogRoot, "series")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	bySlug := map[string]*SeriesMeta{}
	var slugs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		slug := e.Name()
		src, err := os.ReadFile(filepath.Join(root, slug, "index.org"))
		if err != nil {
			continue
		}
		m := parseMeta(string(src))
		title := m.Title
		if title == "" {
			title = slug
		}
		bySlug[slug] = &SeriesMeta{
			Slug:  slug,
			Title: title,
			Tags:  m.Tags,
			Intro: renderOrg(string(src), "series/"+slug),
		}
		slugs = append(slugs, slug)
	}

	chapters := map[string][]PostMeta{}
	for _, p := range posts {
		if p.Series != "" {
			chapters[p.Series] = append(chapters[p.Series], p)
		}
	}

	series := make([]SeriesMeta, 0, len(slugs))
	for _, slug := range slugs {
		sm := bySlug[slug]
		chs := chapters[slug]
		sort.Slice(chs, func(i, j int) bool { return chs[i].SeriesOrder < chs[j].SeriesOrder })
		sm.Start, sm.Latest = seriesRange(chs)
		sm.Count = len(chs)
		sm.Posts = make([]postSummary, 0, len(chs))
		for _, c := range chs {
			sm.Posts = append(sm.Posts, summarize(c))
		}
		series = append(series, *sm)
	}
	sort.Slice(series, func(i, j int) bool {
		ti, _ := parsePostTime(PostMeta{Date: series[i].Latest})
		tj, _ := parsePostTime(PostMeta{Date: series[j].Latest})
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return series[i].Title < series[j].Title
	})
	return series, nil
}

// seriesRange returns the earliest and latest chapter dates, in display form.
func seriesRange(chapters []PostMeta) (start, latest string) {
	var startT, latestT time.Time
	for _, c := range chapters {
		t, ok := parsePostTime(c)
		if !ok {
			continue
		}
		d := displayDate(c.Date)
		if start == "" || t.Before(startT) {
			start, startT = d, t
		}
		if latest == "" || t.After(latestT) {
			latest, latestT = d, t
		}
	}
	return start, latest
}

// attachSeriesTitles fills SeriesTitle on series chapters so chapter pages
// can show the series name.
func attachSeriesTitles(posts []PostMeta, series []SeriesMeta) {
	m := make(map[string]string, len(series))
	for _, sm := range series {
		m[sm.Slug] = sm.Title
	}
	for i := range posts {
		if posts[i].Series != "" && posts[i].SeriesTitle == "" {
			posts[i].SeriesTitle = m[posts[i].Series]
		}
	}
}

// fillFallbacks derives Title/Date from content/path when keywords are absent.
func (m *PostMeta) fillFallbacks() {
	if m.Date != "" {
		return
	}
	parts := strings.Split(m.Path, "/")
	if len(parts) >= 2 && yearPartRe.MatchString(parts[0]) {
		m.Date = parts[0] + "-" + parts[1]
	}
}

func fallbackTitle(src string, pathSlug string) string {
	if m := headingRe.FindStringSubmatch(src); m != nil {
		return strings.TrimSpace(m[2])
	}
	return filepath.Base(pathSlug)
}

var dateLayouts = []string{
	"2006-01-02", "2006-01-02 15:04", "2006-01-02 15:04:05",
	"2006-01-02T15:04:05", "2006/01/02", "2006.01.02",
}

// parsePostTime tries the DATE field, then the YYYY/MM path prefix.
func parsePostTime(meta PostMeta) (time.Time, bool) {
	d := strings.TrimSpace(meta.Date)
	d = strings.Trim(d, "<>[]")
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, d); err == nil {
			return t, true
		}
	}
	parts := strings.Split(meta.Path, "/")
	if len(parts) >= 2 {
		if t, err := time.Parse("2006-01", parts[0]+"-"+parts[1]); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func sortPosts(posts []PostMeta) {
	sort.Slice(posts, func(a, b int) bool {
		ta, oka := parsePostTime(posts[a])
		tb, okb := parsePostTime(posts[b])
		if oka && okb && !ta.Equal(tb) {
			return ta.After(tb)
		}
		if oka != okb {
			return oka
		}
		return posts[a].Path > posts[b].Path
	})
}

func displayDate(d string) string {
	return strings.TrimSpace(strings.Trim(d, "<>[]"))
}

// ---------------------------------------------------------------------------
// API handlers
// ---------------------------------------------------------------------------

type postSummary struct {
	Path  string   `json:"path"`
	Title string   `json:"title"`
	Date  string   `json:"date"`
	Tags  []string `json:"tags"`
	Order int      `json:"order,omitempty"` // series chapter order (0 for normal posts)
}

func summarize(p PostMeta) postSummary {
	return postSummary{Path: p.Path, Title: p.Title, Date: displayDate(p.Date), Tags: p.Tags, Order: p.SeriesOrder}
}

// feedItem is one entry in posts.json: either a standalone post (kind=post)
// or a whole series collapsed into a single row (kind=series).
type feedItem struct {
	Kind   string   `json:"kind"` // "post" | "series"
	Path   string   `json:"path,omitempty"`
	Series string   `json:"series,omitempty"`
	Title  string   `json:"title"`
	Date   string   `json:"date,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Count  int      `json:"count,omitempty"`

	t time.Time // sort key, not serialized
}

// buildFeed merges standalone posts and series entries into a single list
// ordered by date, newest first. Series chapters are folded into their series.
func buildFeed(posts []PostMeta, series []SeriesMeta) []feedItem {
	items := make([]feedItem, 0, len(posts)+len(series))
	for _, p := range posts {
		if p.Series != "" {
			continue
		}
		t, _ := parsePostTime(p)
		items = append(items, feedItem{
			Kind: "post", Path: p.Path, Title: p.Title,
			Date: displayDate(p.Date), Tags: p.Tags, t: t,
		})
	}
	for _, sm := range series {
		t, _ := parsePostTime(PostMeta{Date: sm.Latest})
		items = append(items, feedItem{
			Kind: "series", Series: sm.Slug, Title: sm.Title,
			Date: sm.Latest, Tags: sm.Tags, Count: sm.Count, t: t,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].t.Equal(items[j].t) {
			return items[i].t.After(items[j].t)
		}
		return items[i].Title < items[j].Title
	})
	return items
}

func (s *server) handlePosts(w http.ResponseWriter, r *http.Request) {
	posts, err := s.scanPosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	series, err := s.scanSeries(posts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := buildFeed(posts, series)
	tag := r.URL.Query().Get("tag")
	list := make([]feedItem, 0, len(items))
	for _, it := range items {
		if tag != "" && !hasTag(it.Tags, tag) {
			continue
		}
		list = append(list, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": list})
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}

var postSlugRe = regexp.MustCompile(`^(\d{4}/\d{2}/[A-Za-z0-9._-]+|series/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+)$`)

func (s *server) handlePost(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Query().Get("p"), "/")
	if !postSlugRe.MatchString(p) {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	meta, htmlOut, err := s.loadPost(p)
	if err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	posts, err := s.scanPosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	prev, next := findNav(posts, p)
	detail := struct {
		postSummary
		HTML string       `json:"html"`
		Prev *postSummary `json:"prev"`
		Next *postSummary `json:"next"`
	}{postSummary: summarize(meta), HTML: htmlOut, Prev: prev, Next: next}
	writeJSON(w, http.StatusOK, detail)
}

// findNav returns the neighbours of path. Standalone posts navigate by date
// across the published list; series chapters navigate within their series
// (by NN order).
func findNav(posts []PostMeta, path string) (prev, next *postSummary) {
	idx := -1
	for i, q := range posts {
		if q.Path == path {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil
	}
	cur := posts[idx]

	var list []PostMeta
	if cur.Series != "" {
		for _, q := range posts {
			if q.Series == cur.Series {
				list = append(list, q)
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].SeriesOrder < list[j].SeriesOrder })
	} else {
		for _, q := range posts {
			if q.Series == "" {
				list = append(list, q)
			}
		}
		// posts are already date-descending; list keeps that order
	}

	k := -1
	for i, q := range list {
		if q.Path == path {
			k = i
			break
		}
	}
	if k < 0 {
		return nil, nil
	}
	if k > 0 {
		ps := summarize(list[k-1])
		prev = &ps
	}
	if k < len(list)-1 {
		ps := summarize(list[k+1])
		next = &ps
	}
	return prev, next
}

// loadPost reads and renders a single post (works for drafts too, for preview).
func (s *server) loadPost(slug string) (PostMeta, string, error) {
	full := filepath.Join(s.blogRoot, filepath.FromSlash(slug+".org"))
	src, err := os.ReadFile(full)
	if err != nil {
		return PostMeta{}, "", err
	}
	meta := parseMeta(string(src))
	meta.Path = slug
	s.applySeriesPath(&meta)
	meta.fillFallbacks()
	if meta.Title == "" {
		meta.Title = fallbackTitle(string(src), slug)
	}
	dir := strings.TrimSuffix(slug, "/"+filepath.Base(slug))
	dir = strings.TrimSuffix(dir, "/")
	htmlOut := renderOrg(string(src), dir)
	return meta, htmlOut, nil
}

// applySeriesPath fills Series/SeriesTitle when slug is a series chapter.
func (s *server) applySeriesPath(meta *PostMeta) {
	parts := strings.Split(meta.Path, "/")
	if len(parts) != 3 || parts[0] != "series" {
		return
	}
	meta.Series = parts[1]
	meta.SeriesTitle = s.seriesTitle(parts[1])
}

// seriesTitle reads the display title from a series index.org, falling back
// to the slug.
func (s *server) seriesTitle(slug string) string {
	src, err := os.ReadFile(filepath.Join(s.blogRoot, "series", slug, "index.org"))
	if err != nil {
		return slug
	}
	m := parseMeta(string(src))
	if m.Title == "" {
		return slug
	}
	return m.Title
}

type tagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

func buildTags(posts []PostMeta, series []SeriesMeta) []tagCount {
	counts := map[string]int{}
	for _, p := range posts {
		if p.Series != "" {
			continue // chapters are covered by their series entry
		}
		for _, t := range p.Tags {
			counts[t]++
		}
	}
	for _, sm := range series {
		for _, t := range sm.Tags {
			counts[t]++
		}
	}
	list := make([]tagCount, 0, len(counts))
	for t, c := range counts {
		list = append(list, tagCount{Tag: t, Count: c})
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].Count != list[b].Count {
			return list[a].Count > list[b].Count
		}
		return list[a].Tag < list[b].Tag
	})
	return list
}

func (s *server) handleTags(w http.ResponseWriter, r *http.Request) {
	posts, err := s.scanPosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	series, err := s.scanSeries(posts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": buildTags(posts, series)})
}

// buildAbout renders blog/about.org into the {title, html} shape used by
// both the API and the static generator.
func (s *server) buildAbout() (map[string]string, error) {
	src, err := os.ReadFile(filepath.Join(s.blogRoot, "about.org"))
	if err != nil {
		return nil, err
	}
	meta := parseMeta(string(src))
	title := meta.Title
	if title == "" {
		title = fallbackTitle(string(src), "about")
	}
	if title == "" || title == "about" {
		title = "About"
	}
	return map[string]string{
		"title": title,
		"html":  renderOrg(string(src), ""),
	}, nil
}

func (s *server) handleAbout(w http.ResponseWriter, r *http.Request) {
	about, err := s.buildAbout()
	if err != nil {
		writeErr(w, http.StatusNotFound, "about.org not found")
		return
	}
	writeJSON(w, http.StatusOK, about)
}

// ---------------------------------------------------------------------------
// static files
// ---------------------------------------------------------------------------

func (s *server) handleStatic(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/":
		s.serveFile(w, r, filepath.Join(s.frontendRoot, "index.html"))
	case p == "/blog" || p == "/tags" || p == "/about":
		s.serveFile(w, r, filepath.Join(s.frontendRoot, strings.TrimPrefix(p, "/")+".html"))
	case strings.HasPrefix(p, "/t/") && len(p) > 3:
		s.serveFile(w, r, filepath.Join(s.frontendRoot, "tags.html"))
	case strings.HasPrefix(p, "/posts/") && len(p) > 8:
		s.handlePostPage(w, r)
	case strings.HasPrefix(p, "/series/") && strings.HasSuffix(p, ".json"):
		s.handleSeriesJSON(w, r)
	case strings.HasPrefix(p, "/series/") && strings.HasSuffix(p, ".html"):
		s.handleSeriesPage(w, r)
	case strings.HasPrefix(p, "/assets/"):
		rel := filepath.FromSlash(strings.TrimPrefix(p, "/assets/"))
		full, ok := safeJoin(filepath.Join(s.blogRoot, "assets"), rel)
		if !ok {
			s.notFound(w, r)
			return
		}
		s.serveFile(w, r, full)
	default:
		rel := filepath.FromSlash(strings.TrimPrefix(p, "/"))
		full, ok := safeJoin(s.frontendRoot, rel)
		if !ok {
			s.notFound(w, r)
			return
		}
		if _, err := os.Stat(full); err != nil {
			s.notFound(w, r)
			return
		}
		s.serveFile(w, r, full)
	}
}

func (s *server) serveFile(w http.ResponseWriter, r *http.Request, file string) {
	f, err := os.Open(file)
	if err != nil {
		s.notFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		s.notFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(file), st.ModTime(), f)
}

// seriesSlugFromPath extracts the series slug from /series/SLUG[.suffix].
func seriesSlugFromPath(p, suffix string) string {
	rest := strings.TrimPrefix(p, "/series/")
	rest = strings.TrimSuffix(rest, suffix)
	if rest == "" || !seriesSlugRe.MatchString(rest) {
		return ""
	}
	return rest
}

// handleSeriesPage serves the static shell for a series landing page. The
// data is fetched by js/series.js from /series/SLUG.json.
func (s *server) handleSeriesPage(w http.ResponseWriter, r *http.Request) {
	slug := seriesSlugFromPath(r.URL.Path, ".html")
	if slug == "" {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(seriesPageShell())); err != nil {
		log.Printf("handleSeriesPage: write: %v", err)
	}
}

// handleSeriesJSON serves the data for one series landing page.
func (s *server) handleSeriesJSON(w http.ResponseWriter, r *http.Request) {
	slug := seriesSlugFromPath(r.URL.Path, ".json")
	if slug == "" {
		s.notFound(w, r)
		return
	}
	posts, err := s.scanPosts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	series, err := s.scanSeries(posts)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, sm := range series {
		if sm.Slug == slug {
			writeJSON(w, http.StatusOK, sm)
			return
		}
	}
	writeErr(w, http.StatusNotFound, "series not found")
}

// seriesPageShell is the static HTML shell shared by generate and the dev
// server. The slug is resolved on the client from location.pathname.
func seriesPageShell() string {
	return `<!doctype html>
<html>
<head>
<meta charset="UTF-8"/>
<title>series - zal-blog</title>
<link href="/style.css" rel="stylesheet"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
</head>
<body>
<div class="term-frame">
<span class="blog-title" id="series-title">series</span>
<hr class="splitter" />
<a href="/">back index</a>
<a href="/blog.html">visit blog</a>
<a href="/tags.html">tags</a>
<div class="content" id="series-content"></div>
</div>
<script type="module" src="/js/series.js"></script>
</body>
</html>
`
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	_ = r
	body, err := os.ReadFile(filepath.Join(s.frontendRoot, "404.html"))
	if err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if _, err := w.Write(body); err != nil {
		log.Printf("notFound: write: %v", err)
	}
}

// safeJoin joins root and rel ensuring the result stays inside root.
func safeJoin(root, rel string) (string, bool) {
	clean := filepath.Clean(filepath.Join(root, rel))
	if clean == root {
		return clean, true
	}
	if strings.HasPrefix(clean, root+string(os.PathSeparator)) {
		return clean, true
	}
	return "", false
}

package main

import "testing"

func TestClassifyPost(t *testing.T) {
	cases := []struct {
		rel    string
		suffix bool // whether the series uses suffix ordering
		wantOK bool
		path   string
		series string
		order  int
	}{
		{"2026/09/oi-templates.org", false, true, "2026/09/oi-templates", "", 0},
		{"series/sicp-notes/01-chapter-one.org", false, true, "series/sicp-notes/01-chapter-one", "sicp-notes", 1},
		{"series/sicp-notes/10-appendix.org", false, true, "series/sicp-notes/10-appendix", "sicp-notes", 10},
		{"series/sicp-notes/foo.org", false, false, "", "", 0},     // no number → not a chapter
		{"series/sicp-notes/index.org", false, false, "", "", 0},  // series index
		{"series/ABC/Atcoder-Beginner-Contest-475.org", true, true, "series/ABC/Atcoder-Beginner-Contest-475", "ABC", 475},
		{"series/ABC/Atcoder-Beginner-Contest-457.org", true, true, "series/ABC/Atcoder-Beginner-Contest-457", "ABC", 457},
		{"series/ABC/index.org", true, false, "", "", 0},          // suffix series index
		{"about.org", false, false, "", "", 0},
	}
	for _, c := range cases {
		suffixBySlug := map[string]bool{}
		if c.series != "" {
			suffixBySlug[c.series] = c.suffix
		}
		m, ok := classifyPost(c.rel, suffixBySlug)
		if ok != c.wantOK {
			t.Errorf("classifyPost(%q) ok = %v, want %v", c.rel, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if m.Path != c.path || m.Series != c.series || m.SeriesOrder != c.order {
			t.Errorf("classifyPost(%q) = %+v", c.rel, m)
		}
	}
}

func TestSeriesRange(t *testing.T) {
	chapters := []PostMeta{
		{Date: "2026-09-14"},
		{Date: "2026-09-12"},
		{Date: ""},
	}
	start, latest := seriesRange(chapters)
	if start != "2026-09-12" || latest != "2026-09-14" {
		t.Errorf("seriesRange = (%q, %q), want (2026-09-12, 2026-09-14)", start, latest)
	}
}

func TestBuildFeedFoldsSeries(t *testing.T) {
	posts := []PostMeta{
		{Path: "2026/08/a", Title: "A", Date: "2026-08-01"},
		{Path: "series/sicp/chapter-one", Title: "ch1", Date: "2026-09-14", Series: "sicp", SeriesOrder: 1},
		{Path: "series/sicp/chapter-two", Title: "ch2", Date: "2026-09-15", Series: "sicp", SeriesOrder: 2},
	}
	series := []SeriesMeta{
		{Slug: "sicp", Title: "SICP", Tags: []string{"cs"}, Latest: "2026-09-15", Count: 2},
	}
	items := buildFeed(posts, series)
	if len(items) != 2 {
		t.Fatalf("buildFeed len = %d, want 2", len(items))
	}
	if items[0].Kind != "series" || items[0].Count != 2 || items[0].Series != "sicp" {
		t.Errorf("items[0] = %+v, want series entry", items[0])
	}
	if items[1].Kind != "post" || items[1].Path != "2026/08/a" {
		t.Errorf("items[1] = %+v, want post", items[1])
	}
	for _, it := range items {
		if it.Path == "series/sicp/chapter-one" || it.Path == "series/sicp/chapter-two" {
			t.Errorf("chapter leaked into feed: %+v", it)
		}
	}
}

func TestFindNavWithinSeries(t *testing.T) {
	posts := []PostMeta{
		{Path: "series/sicp/chapter-two", Series: "sicp", SeriesOrder: 2, Title: "Two"},
		{Path: "series/sicp/chapter-one", Series: "sicp", SeriesOrder: 1, Title: "One"},
		{Path: "series/sicp/chapter-three", Series: "sicp", SeriesOrder: 3, Title: "Three"},
		{Path: "2026/08/a", Title: "A"},
	}
	prev, next := findNav(posts, "series/sicp/chapter-two")
	if prev == nil || prev.Title != "One" {
		t.Errorf("prev = %+v, want One", prev)
	}
	if next == nil || next.Title != "Three" {
		t.Errorf("next = %+v, want Three", next)
	}
}
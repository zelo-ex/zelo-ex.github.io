package main

import (
	"fmt"
	"html"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// PostMeta is the metadata of a single org post.
type PostMeta struct {
	Path  string   `json:"path"` // relative to blog root, without .org, e.g. "2025/08/hello"
	Title string   `json:"title"`
	Date  string   `json:"date"`
	Tags  []string `json:"tags"`

	// Series fields are empty for normal posts.
	Series      string `json:"series,omitempty"`       // series slug, e.g. "sicp-notes"
	SeriesTitle string `json:"series_title,omitempty"` // display title from series index.org
	SeriesOrder int    `json:"series_order,omitempty"` // 1-based order from the NN- prefix
}

var keywordRe = regexp.MustCompile(`^#\+(\w+):[ \t]*(.*)$`)

// parseMeta extracts #+KEYWORD lines from an org source.
func parseMeta(src string) PostMeta {
	meta := PostMeta{}
	for line := range strings.SplitSeq(src, "\n") {
		m := keywordRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := strings.ToUpper(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "TITLE":
			meta.Title = val
		case "DATE":
			meta.Date = val
		case "TAGS":
			meta.Tags = parseTags(val)
		}
	}
	return meta
}

// parseTags accepts ":a:b:" (org style, colon-delimited).
func parseTags(val string) []string {
	val = strings.TrimSpace(val)
	val = strings.TrimPrefix(val, ":")
	val = strings.TrimSuffix(val, ":")
	fields := strings.FieldsFunc(val, func(r rune) bool {
		return r == ':'
	})
	var tags []string
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		tags = append(tags, f)
	}
	return tags
}

// ---------------------------------------------------------------------------
// org -> HTML renderer
// ---------------------------------------------------------------------------

type orgRenderer struct {
	postDir string // url dir of the post, e.g. "2025/08"; "" for root files (about.org)
	prot    *protected
}

// renderOrg renders org source to an HTML fragment.
// Blocks are emitted with no whitespace between them: the fragment is meant to
// be injected inside a white-space: pre-wrap container.
func renderOrg(src, postDir string) string {
	r := &orgRenderer{postDir: postDir}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "#+begin_src"):
			lang := ""
			if f := strings.Fields(trimmed); len(f) > 1 {
				lang = f[1]
			}
			block, next := collectBlock(lines, i, "#+end_src")
			out = append(out, srcBlock(block, lang))
			i = next
		case strings.HasPrefix(lower, "#+begin_example"):
			block, next := collectBlock(lines, i, "#+end_example")
			out = append(out, srcBlock(block, ""))
			i = next
		case strings.HasPrefix(lower, "#+begin_quote"):
			block, next := collectBlock(lines, i, "#+end_quote")
			out = append(out, "<blockquote>"+renderOrg(strings.Join(block, "\n"), postDir)+"</blockquote>")
			i = next
		case strings.HasPrefix(lower, "#+begin_"):
			// unknown block: skip until matching end, don't render
			name := strings.Fields(lower)[0]
			_, next := collectBlock(lines, i, "#+end_"+strings.TrimPrefix(name, "#+begin_"))
			i = next
		case strings.HasPrefix(lower, "#+"):
			i++ // keyword line, skip
		case headingRe.MatchString(line):
			out = append(out, r.heading(line))
			i++
		case hrRe.MatchString(trimmed):
			out = append(out, "<hr>")
			i++
		case isTableLine(line):
			var tbl []string
			for i < len(lines) && isTableLine(lines[i]) {
				tbl = append(tbl, lines[i])
				i++
			}
			out = append(out, r.table(tbl))
		case isListLine(line):
			items, next := collectList(lines, i)
			out = append(out, r.renderList(items))
			i = next
		default:
			var para []string
			for i < len(lines) && !isBlockStart(lines[i]) {
				para = append(para, strings.TrimSpace(lines[i]))
				i++
			}
			out = append(out, "<p>"+r.inline(strings.Join(para, "\n"))+"</p>")
		}
	}
	return strings.Join(out, "")
}

func collectBlock(lines []string, start int, end string) ([]string, int) {
	var block []string
	i := start + 1
	for ; i < len(lines); i++ {
		l := strings.ToLower(strings.TrimSpace(lines[i]))
		if strings.HasPrefix(l, end) {
			return block, i + 1
		}
		block = append(block, lines[i])
	}
	return block, i
}

func srcBlock(block []string, lang string) string {
	content := strings.Join(block, "\n")
	content = strings.TrimRight(content, "\n")
	class := ""
	if lang != "" {
		class = ` class="language-` + html.EscapeString(lang) + `"`
	}
	return `<details><summary>Code<button class="copy-code">copy</button>` +
		`</summary><pre class="src"><code` + class + `>` +
		html.EscapeString(content) +
		`</code></pre></details>`
}

var (
	headingRe   = regexp.MustCompile(`^(\*{1,5})\s+(.+)$`)
	hrRe        = regexp.MustCompile(`^-{5,}\s*$`)
	ulRe        = regexp.MustCompile(`^(\s*)([-+])\s+(.*)$`)
	olRe        = regexp.MustCompile(`^(\s*)(\d+)[.)]\s+(.*)$`)
	tableLineRe = regexp.MustCompile(`^\s*\|`)
	todoDoneRe  = regexp.MustCompile(`^(TODO|DONE)\s+`)
)

func isTableLine(line string) bool { return tableLineRe.MatchString(line) }

func isListLine(line string) bool {
	return ulRe.MatchString(line) || olRe.MatchString(line)
}

func isBlockStart(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return true
	}
	l := strings.ToLower(t)
	return headingRe.MatchString(line) ||
		hrRe.MatchString(t) ||
		isTableLine(line) ||
		isListLine(line) ||
		strings.HasPrefix(l, "#+")
}

func (r *orgRenderer) heading(line string) string {
	m := headingRe.FindStringSubmatch(line)
	t := todoDoneRe.FindStringSubmatch(m[2])
	level := min(6, len(m[1])+1) // * -> h2, ***** -> h6
	prefix := ""
	if len(t) >= 1 {
		prefix = fmt.Sprintf("<span class=\"status_%s\">%s</span>",
			strings.ToLower(t[1]), t[1])
		m[2], _ = strings.CutPrefix(m[2], t[1])
	}
	return fmt.Sprintf("<h%d>%s%s</h%d>",
		level, prefix, r.inline(m[2]), level)
}

// ---------------------------------------------------------------------------
// table
// ---------------------------------------------------------------------------

func (r *orgRenderer) table(lines []string) string {
	var rows [][]string
	for _, line := range lines {
		cells := strings.Split(strings.TrimSpace(line), "|")
		if len(cells) > 0 && cells[0] == "" {
			cells = cells[1:]
		}
		if len(cells) > 0 && cells[len(cells)-1] == "" {
			cells = cells[:len(cells)-1]
		}
		rows = append(rows, cells)
	}
	var b strings.Builder
	b.WriteString("<table>")
	headerDone := false
	for _, row := range rows {
		if isTableSep(row) {
			continue
		}
		tag := "td"
		if !headerDone {
			tag = "th"
			headerDone = true
		}
		b.WriteString("<tr>")
		for _, cell := range row {
			b.WriteString("<")
			b.WriteString(tag)
			b.WriteString(">")
			b.WriteString(r.inline(strings.TrimSpace(cell)))
			b.WriteString("</")
			b.WriteString(tag)
			b.WriteString(">")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
	return b.String()
}

func isTableSep(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		c = strings.TrimPrefix(strings.TrimSuffix(c, ":"), ":")
		c = strings.ReplaceAll(c, "+", "")
		if len(c) < 3 || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// lists
// ---------------------------------------------------------------------------

type listItem struct {
	indent   int
	ordered  bool
	content  string
	children []*listItem
}

func matchListItem(line string) (indent int, ordered bool, content string, ok bool) {
	if m := ulRe.FindStringSubmatch(line); m != nil {
		return len(m[1]), false, m[3], true
	}
	if m := olRe.FindStringSubmatch(line); m != nil {
		return len(m[1]), true, m[3], true
	}
	return 0, false, "", false
}

func collectList(lines []string, start int) ([]*listItem, int) {
	var items []*listItem
	i := start
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			break
		}
		if ind, ord, content, ok := matchListItem(line); ok {
			items = append(items, &listItem{indent: ind, ordered: ord, content: content})
			i++
			continue
		}
		if len(items) > 0 && !isBlockStart(line) &&
			len(line)-len(strings.TrimLeft(line, " \t")) > items[len(items)-1].indent {
			// continuation line of the last item
			last := items[len(items)-1]
			last.content += "\n" + trimmed
			i++
			continue
		}
		break
	}
	return buildListTree(items), i
}

func buildListTree(items []*listItem) []*listItem {
	var roots []*listItem
	var stack []*listItem
	for _, it := range items {
		for len(stack) > 0 && stack[len(stack)-1].indent >= it.indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, it)
		} else {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, it)
		}
		stack = append(stack, it)
	}
	return roots
}

func (r *orgRenderer) renderList(items []*listItem) string {
	var b strings.Builder
	for i := 0; i < len(items); {
		j := i
		for j < len(items) && items[j].ordered == items[i].ordered {
			j++
		}
		tag := "ul"
		if items[i].ordered {
			tag = "ol"
		}
		b.WriteString("<")
		b.WriteString(tag)
		b.WriteString(">")
		for k := i; k < j; k++ {
			b.WriteString(r.renderItem(items[k]))
		}
		b.WriteString("</")
		b.WriteString(tag)
		b.WriteString(">")
		i = j
	}
	return b.String()
}

func (r *orgRenderer) renderItem(it *listItem) string {
	var b strings.Builder
	b.WriteString("<li>")
	b.WriteString(r.inline(it.content))
	if len(it.children) > 0 {
		b.WriteString(r.renderList(it.children))
	}
	b.WriteString("</li>")
	return b.String()
}

// ---------------------------------------------------------------------------
// inline markup
// ---------------------------------------------------------------------------

var (
	codeTildeRe = regexp.MustCompile(`~([^~\s][^~]*)~`)
	codeEqRe    = regexp.MustCompile(`=([^=\s][^=]*)=`)
	linkDescRe  = regexp.MustCompile(`\[\[([^\[\]]+)\]\[([^\[\]]+)\]\]`)
	linkRe      = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)
	urlRe       = regexp.MustCompile(`https?://[^\s<>"']+`)
)

// inline escapes text then applies: code spans, links/images, bare URLs,
// emphasis (bold, italic, underline, strike).
func (r *orgRenderer) inline(s string) string {
	s = html.EscapeString(s)
	s = r.protectCode(s)
	s = r.protectLinks(s)
	s = emphasisPass(s, '*', "<strong>", "</strong>")
	s = emphasisPass(s, '/', "<em>", "</em>")
	s = emphasisPass(s, '_', "<u>", "</u>")
	s = emphasisPass(s, '+', "<del>", "</del>")
	s = r.restoreProtected(s)
	return s
}

// protected holds generated HTML snippets while emphasis runs.
// Placeholders \x01N\x01 protect them from further markup passes.
type protected struct {
	html []string
}

func (r *orgRenderer) protectCode(s string) string {
	if r.prot == nil {
		r.prot = &protected{}
	}
	store := func(m string) string {
		r.prot.html = append(r.prot.html, "<code>"+m+"</code>")
		return fmt.Sprintf("\x01%d\x01", len(r.prot.html)-1)
	}
	s = codeTildeRe.ReplaceAllStringFunc(s, func(m string) string {
		return store(codeTildeRe.FindStringSubmatch(m)[1])
	})
	s = codeEqRe.ReplaceAllStringFunc(s, func(m string) string {
		return store(codeEqRe.FindStringSubmatch(m)[1])
	})
	return s
}

func (r *orgRenderer) protectLinks(s string) string {
	s = linkDescRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := linkDescRe.FindStringSubmatch(m)
		r.prot.html = append(r.prot.html, r.linkHTML(sub[1], sub[2]))
		return fmt.Sprintf("\x01%d\x01", len(r.prot.html)-1)
	})
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		r.prot.html = append(r.prot.html, r.linkHTML(sub[1], ""))
		return fmt.Sprintf("\x01%d\x01", len(r.prot.html)-1)
	})
	// bare http(s) URLs, trailing punctuation left outside the link
	s = urlRe.ReplaceAllStringFunc(s, func(m string) string {
		u := strings.TrimRight(m, ".,;:!?)]}")
		rest := m[len(u):]
		openNewWindow := ""
		if strings.HasPrefix(u, "http://") ||
			strings.HasPrefix(u, "https://") {
			openNewWindow = ` target="_blank"`
		}
		r.prot.html = append(r.prot.html,
			`<a href="`+u+`"`+openNewWindow+`>`+u+`</a>`)
		return fmt.Sprintf("\x01%d\x01", len(r.prot.html)-1) + rest
	})
	return s
}

func (r *orgRenderer) restoreProtected(s string) string {
	return protectedRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := protectedRe.FindStringSubmatch(m)
		var idx int
		if _, err := fmt.Sscanf(sub[1], "%d", &idx); err != nil {
			return m
		}
		if idx >= 0 && idx < len(r.prot.html) {
			return r.prot.html[idx]
		}
		return m
	})
}

var protectedRe = regexp.MustCompile(`\x01(\d+)\x01`)

// linkHTML builds <a>/<img> for [[target]] or [[target][desc]].
func (r *orgRenderer) linkHTML(target, desc string) string {
	target = strings.TrimSpace(target)
	if rest, ok := strings.CutPrefix(target, "file:"); ok {
		alt := desc
		if alt == "" {
			alt = path.Base(rest)
		}
		src := assetPath(r.postDir, rest)
		return `<img src="` + src + `" alt="` + alt + `">`
	}
	text := desc
	if text == "" {
		text = target
	}
	openNewWindow := ""
	if strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") {
		openNewWindow = ` target="_blank"`
	}
	return `<a href="` + target + `"` + openNewWindow + `>` + text + `</a>`
}

// assetPath resolves a file: reference relative to the post's directory.
// Assets live under blog/assets/ mirroring the post hierarchy:
// a post at 2025/08/x.org referencing [[file:pic.png]] maps to
// /assets/2025/08/pic.png. Absolute references starting with "/" are kept.
func assetPath(postDir, ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "./")
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "/") {
		return path.Clean(ref)
	}
	return path.Clean("/assets/" + postDir + "/" + ref)
}

// emphasisPass wraps valid *bold*, /italic/, _underline_, +strike+ spans.
// A span is valid when the marker is not adjacent to a word character and
// the content has no leading/trailing whitespace.
func emphasisPass(s string, marker byte, open, close string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != marker {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := strings.IndexByte(s[i+1:], marker)
		if j < 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		j += i + 1
		content := s[i+1 : j]
		if content == "" || strings.TrimSpace(content) != content ||
			!boundaryBefore(s, i) || !boundaryAfter(s, j) {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteString(open)
		b.WriteString(content)
		b.WriteString(close)
		i = j + 1
	}
	return b.String()
}

func boundaryBefore(s string, idx int) bool {
	if idx == 0 {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:idx])
	return !isWordRune(prev)
}

func boundaryAfter(s string, idx int) bool {
	if idx >= len(s)-1 {
		return true
	}
	next, _ := utf8.DecodeRuneInString(s[idx+1:])
	return !isWordRune(next)
}

// isWordRune blocks emphasis next to ascii word chars; CJK characters may
// sit right next to markers (e.g. 你好*world*).
func isWordRune(r rune) bool {
	return r == '_' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r > 127)
}

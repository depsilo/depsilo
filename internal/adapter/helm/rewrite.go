package helm

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
)

// helmRewriteBufferSize bounds one buffered index line. A longer line, which
// no generated chart URL comes near, passes through unchanged.
const helmRewriteBufferSize = 64 << 10

// rewriteHelmIndex streams a repository index and turns every chart URL that
// lies under repoURL, the directory the index was fetched from, into the
// equivalent path relative to it. Helm resolves a relative chart URL against
// the repository URL the client added, so the download then reaches Depsilo
// instead of the Upstream, whatever route the repository was added under.
// URLs on another host or outside that directory are left as they are, and
// every byte other than a rewritten URL is preserved; digests describe the
// chart archives, not the index, so they stay valid.
func rewriteHelmIndex(body io.ReadCloser, repoURL string) io.ReadCloser {
	base, err := url.Parse(repoURL)
	if err != nil || !helmRewritableBase(base) {
		return body
	}
	return &helmIndexRewriter{
		source:            body,
		reader:            bufio.NewReaderSize(body, helmRewriteBufferSize),
		base:              base,
		urlsColumn:        -1,
		blockScalarIndent: -1,
	}
}

func helmRewritableBase(base *url.URL) bool {
	scheme := strings.ToLower(base.Scheme)
	return (scheme == "http" || scheme == "https") && base.Host != "" &&
		base.Opaque == "" && base.RawQuery == "" && base.Fragment == ""
}

// helmIndexRewriter is a pull-based line transformer: each Read consumes at
// most one buffered line of the source, so memory stays bounded by
// helmRewriteBufferSize whatever the index size.
type helmIndexRewriter struct {
	source  io.ReadCloser
	reader  *bufio.Reader
	base    *url.URL
	pending []byte
	err     error
	// midLine is set while the rest of an overlong line passes through.
	midLine bool

	inEntries bool
	// urlsColumn is the column of an open block `urls:` key, or -1.
	urlsColumn int
	// blockScalarIndent is the key column of an open `|` or `>` block
	// scalar, whose more-indented lines are text, or -1.
	blockScalarIndent int
}

func (r *helmIndexRewriter) Read(p []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		line, err := r.reader.ReadSlice('\n')
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			if !r.midLine {
				r.observe(line, false)
			}
			r.pending, r.midLine = line, true
		case r.midLine:
			r.pending, r.midLine = line, false
			r.err = err
		default:
			if len(line) > 0 {
				r.pending = r.observe(line, true)
			}
			r.err = err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *helmIndexRewriter) Close() error { return r.source.Close() }

// observe advances the index state with one line and returns the line to
// emit. An incomplete line updates the state but is never rewritten.
func (r *helmIndexRewriter) observe(raw []byte, complete bool) []byte {
	content := bytes.TrimRight(raw, "\r\n")
	if !complete {
		content = raw
	}
	line := string(content)
	indent := helmLineIndent(line)
	body := strings.TrimSpace(line)
	if body == "" || strings.HasPrefix(body, "#") {
		return raw
	}
	if r.blockScalarIndent >= 0 {
		if indent > r.blockScalarIndent {
			return raw
		}
		r.blockScalarIndent = -1
	}
	if indent == 0 {
		r.urlsColumn = -1
		key, value, _ := helmSplitKey(body)
		r.inEntries = key == "entries"
		if helmOpensBlockScalar(value) {
			r.blockScalarIndent = 0
		}
		return raw
	}
	if !r.inEntries {
		return raw
	}
	if r.urlsColumn >= 0 {
		if indent >= r.urlsColumn && helmIsListMarker(body) {
			if !complete {
				return raw
			}
			return r.rewriteBlockItem(raw, line, indent)
		}
		r.urlsColumn = -1
	}

	// Strip sequence markers so `- urls:` and `- description: |` are seen
	// at the column of their key.
	column, rest := indent, line[indent:]
	for helmIsListMarker(rest) {
		trimmed := strings.TrimLeft(rest[1:], " ")
		column += len(rest) - len(trimmed)
		rest = trimmed
	}
	key, value, ok := helmSplitKey(rest)
	if !ok {
		return raw
	}
	if helmOpensBlockScalar(value) {
		r.blockScalarIndent = column
		return raw
	}
	if key != "urls" {
		return raw
	}
	value = helmStripComment(value)
	switch {
	case value == "":
		r.urlsColumn = column
	case strings.HasPrefix(value, "[") && complete:
		return r.rewriteFlowList(raw, line, len(line)-len(rest))
	}
	return raw
}

// rewriteBlockItem rewrites the scalar of one `- <url>` line.
func (r *helmIndexRewriter) rewriteBlockItem(raw []byte, line string, indent int) []byte {
	start := indent + 1
	for start < len(line) && line[start] == ' ' {
		start++
	}
	token, ok := helmScanScalar(line, start, false)
	if !ok {
		return raw
	}
	replacement, ok := r.relative(token)
	if !ok {
		return raw
	}
	return helmSplice(raw, line, []helmEdit{{start: token.start, end: token.end, text: replacement}})
}

// rewriteFlowList rewrites the items of a one-line `urls: [a, b]` value. A
// list it cannot parse completely is left untouched.
func (r *helmIndexRewriter) rewriteFlowList(raw []byte, line string, keyStart int) []byte {
	open := strings.IndexByte(line[keyStart:], '[')
	if open < 0 {
		return raw
	}
	position := keyStart + open + 1
	var edits []helmEdit
	for {
		for position < len(line) && line[position] == ' ' {
			position++
		}
		if position < len(line) && line[position] == ']' {
			break
		}
		token, ok := helmScanScalar(line, position, true)
		if !ok {
			return raw
		}
		if replacement, ok := r.relative(token); ok {
			edits = append(edits, helmEdit{start: token.start, end: token.end, text: replacement})
		}
		position = token.end
		for position < len(line) && line[position] == ' ' {
			position++
		}
		if position >= len(line) {
			return raw
		}
		if line[position] == ']' {
			break
		}
		if line[position] != ',' {
			return raw
		}
		position++
	}
	if len(edits) == 0 {
		return raw
	}
	return helmSplice(raw, line, edits)
}

// relative returns the replacement token for a chart URL under the
// repository directory, keeping the original quoting.
func (r *helmIndexRewriter) relative(token helmScalar) (string, bool) {
	relative, ok := helmRelativeChartURL(r.base, token.value)
	if !ok {
		return "", false
	}
	if token.quote != 0 {
		return string(token.quote) + relative + string(token.quote), true
	}
	return relative, true
}

// helmRelativeChartURL returns declared as a path relative to the repository
// directory base when declared is an absolute URL inside it. A URL carrying a
// query, fragment or user information is not rewritten: Helm drops a relative
// reference's query when it resolves one, and credentials must not be
// separated from their origin.
func helmRelativeChartURL(base *url.URL, declared string) (string, bool) {
	parsed, err := url.Parse(declared)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		!strings.EqualFold(parsed.Scheme, base.Scheme) ||
		!strings.EqualFold(helmHostPort(parsed), helmHostPort(base)) {
		return "", false
	}
	prefix := strings.TrimSuffix(base.EscapedPath(), "/") + "/"
	relative, found := strings.CutPrefix(parsed.EscapedPath(), prefix)
	if !found || relative == "" {
		return "", false
	}
	for _, segment := range strings.Split(relative, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "" || decoded == "." || decoded == ".." ||
			strings.ContainsAny(decoded, "/\\") {
			return "", false
		}
	}
	// A first segment that holds a colon would read as a scheme, and a
	// leading YAML indicator would change a plain scalar's meaning.
	first, _, _ := strings.Cut(relative, "/")
	if strings.Contains(first, ":") || !helmPlainStart(relative[0]) {
		relative = "./" + relative
	}
	return relative, true
}

func helmPlainStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.'
}

func helmHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// helmScalar is one YAML scalar token on a line: [start, end) covers the
// token including any quotes.
type helmScalar struct {
	start, end int
	quote      byte
	value      string
}

// helmScanScalar reads a quoted or plain scalar at start. Quoted scalars with
// escapes are refused rather than decoded, so they are never rewritten.
func helmScanScalar(line string, start int, flow bool) (helmScalar, bool) {
	if start >= len(line) {
		return helmScalar{}, false
	}
	if quote := line[start]; quote == '"' || quote == '\'' {
		closing := strings.IndexByte(line[start+1:], quote)
		if closing < 0 {
			return helmScalar{}, false
		}
		end := start + 1 + closing + 1
		value := line[start+1 : end-1]
		if (quote == '"' && strings.Contains(value, `\`)) || (quote == '\'' && end < len(line) && line[end] == '\'') {
			return helmScalar{}, false
		}
		return helmScalar{start: start, end: end, quote: quote, value: value}, true
	}
	end := start
	for end < len(line) {
		c := line[end]
		if flow && (c == ',' || c == ']') {
			break
		}
		if c == '#' && end > start && line[end-1] == ' ' {
			break
		}
		end++
	}
	for end > start && line[end-1] == ' ' {
		end--
	}
	if end == start {
		return helmScalar{}, false
	}
	return helmScalar{start: start, end: end, value: line[start:end]}, true
}

type helmEdit struct {
	start, end int
	text       string
}

// helmSplice applies ordered, non-overlapping edits to line and re-attaches
// raw's original line terminator.
func helmSplice(raw []byte, line string, edits []helmEdit) []byte {
	var out strings.Builder
	out.Grow(len(raw) + 16)
	previous := 0
	for _, edit := range edits {
		out.WriteString(line[previous:edit.start])
		out.WriteString(edit.text)
		previous = edit.end
	}
	out.WriteString(line[previous:])
	out.Write(raw[len(line):])
	return []byte(out.String())
}

// helmSplitKey splits a `key: value` mapping line. The key must be a plain
// scalar; quoted keys are not index fields the rewriter acts on.
func helmSplitKey(body string) (key, value string, ok bool) {
	colon := strings.IndexByte(body, ':')
	if colon <= 0 || (colon+1 < len(body) && body[colon+1] != ' ' && body[colon+1] != '\t') {
		return "", "", false
	}
	key = strings.TrimSpace(body[:colon])
	if strings.ContainsAny(key, `"' `) {
		return "", "", false
	}
	return key, strings.TrimSpace(body[colon+1:]), true
}

func helmStripComment(value string) string {
	if strings.HasPrefix(value, "#") {
		return ""
	}
	if index := strings.Index(value, " #"); index >= 0 && !strings.HasPrefix(value, `"`) && !strings.HasPrefix(value, "'") {
		return strings.TrimSpace(value[:index])
	}
	return value
}

// helmOpensBlockScalar reports whether a mapping value starts a literal or
// folded block scalar, such as `|`, `|-` or `>2`.
func helmOpensBlockScalar(value string) bool {
	value = helmStripComment(value)
	if value == "" || (value[0] != '|' && value[0] != '>') {
		return false
	}
	return strings.Trim(value[1:], "+-0123456789") == ""
}

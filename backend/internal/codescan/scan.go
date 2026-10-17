// Package codescan finds the requirement tags in a checkout's source comments (#201), such as [implements CHK-R42@3],
// for exciter-ci to upload; it reads files and nothing else.
package codescan

import (
	"bufio"
	"bytes"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MaxFileBytes bounds a file the scanner reads; larger ones are generated or data, not source.
const MaxFileBytes = 2 << 20

// binarySniff is how much of a file is looked at for a NUL byte, which marks it as not text.
const binarySniff = 8 << 10

// style is how one language writes comments: line comments, and block comments by their opening and closing.
type style struct {
	line   []string
	blocks [][2]string
}

var (
	cLike  = style{line: []string{"//"}, blocks: [][2]string{{"/*", "*/"}}}
	hash   = style{line: []string{"#"}}
	python = style{line: []string{"#"}, blocks: [][2]string{{`"""`, `"""`}, {"'''", "'''"}}}
	dashes = style{line: []string{"--"}, blocks: [][2]string{{"/*", "*/"}}}
	semi   = style{line: []string{";"}}
	pct    = style{line: []string{"%"}}
	markup = style{blocks: [][2]string{{"<!--", "-->"}}}
	// anything is every style at once, for a file whose kind the scanner does not know.
	anything = style{line: []string{"//", "#", "--", ";", "%"}, blocks: [][2]string{{"/*", "*/"}, {"<!--", "-->"}}}
)

var styles = map[string]style{
	".go": cLike, ".js": cLike, ".jsx": cLike, ".ts": cLike, ".tsx": cLike, ".mjs": cLike, ".cjs": cLike, ".java": cLike,
	".kt": cLike, ".kts": cLike, ".scala": cLike, ".c": cLike, ".h": cLike, ".cc": cLike, ".cpp": cLike, ".hpp": cLike,
	".cs": cLike, ".swift": cLike, ".rs": cLike, ".dart": cLike, ".php": cLike, ".css": cLike, ".scss": cLike, ".groovy": cLike,
	".proto": cLike,
	".py":    python, ".pyi": python,
	".rb": hash, ".sh": hash, ".bash": hash, ".zsh": hash, ".yaml": hash, ".yml": hash, ".toml": hash, ".pl": hash, ".r": hash,
	".ex": hash, ".exs": hash, ".tf": hash, ".cmake": hash, ".ps1": hash, ".mk": hash, ".dockerfile": hash, ".nix": hash,
	".sql": dashes, ".lua": dashes, ".hs": dashes, ".ada": dashes, ".adb": dashes, ".ads": dashes, ".vhd": dashes, ".vhdl": dashes,
	".lisp": semi, ".el": semi, ".clj": semi, ".scm": semi, ".asm": semi, ".s": semi, ".ini": semi,
	".tex": pct, ".m": pct, ".erl": pct, ".hrl": pct,
	".html": markup, ".htm": markup, ".xml": markup, ".md": markup, ".vue": markup, ".svg": markup,
}

// styleOf is the comment style of a file by its name: a Makefile or a Dockerfile writes #, an unknown kind any.
func styleOf(path string) style {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case base == "makefile" || base == "dockerfile" || strings.HasPrefix(base, "dockerfile."):
		return hash
	}
	if s, ok := styles[strings.ToLower(filepath.Ext(path))]; ok {
		return s
	}
	return anything
}

// Found is one tag as the scanner read it: where, its word, the key, and the revision it names, if any; the shape
// a scan uploads.
type Found struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Word     string `json:"word"`
	Key      string `json:"key"`
	Revision *int   `json:"revision,omitempty"`
}

// Scanner finds a project's tags in source comments: a word of the project's, then keys of its own, each with the
// revision it names, as [implements CHK-R42@3, CHK-R43].
type Scanner struct {
	tag  *regexp.Regexp
	keys *regexp.Regexp
}

// NewScanner looks for the words given and keys of the project keys given, a project's former keys included.
func NewScanner(words, projectKeys []string) *Scanner {
	quote := func(list []string) string {
		out := make([]string, 0, len(list))
		for _, w := range list {
			out = append(out, regexp.QuoteMeta(w))
		}
		return strings.Join(out, "|")
	}
	key := `(?:` + quote(projectKeys) + `)-R[1-9][0-9]*(?:@[0-9]+)?`
	return &Scanner{
		tag:  regexp.MustCompile(`(?i)\[\s*(` + quote(words) + `)\s+(` + key + `(?:\s*[,;\s]\s*` + key + `)*)\s*\]`),
		keys: regexp.MustCompile(`(?i)(` + quote(projectKeys) + `)-R([1-9][0-9]*)(?:@([0-9]+))?`),
	}
}

// File reads one file's tags, in its comments only; a file that is not text, or too large, holds none.
func (s *Scanner) File(path string, content []byte) []Found {
	if len(content) > MaxFileBytes || bytes.IndexByte(content[:min(len(content), binarySniff)], 0) >= 0 {
		return nil
	}
	st := styleOf(path)
	var (
		out  []Found
		open = -1
	)
	lines := bufio.NewScanner(bytes.NewReader(content))
	lines.Buffer(make([]byte, 0, 64<<10), MaxFileBytes)
	for n := 1; lines.Scan(); n++ {
		comments, still := commentsOf(lines.Text(), st, open)
		open = still
		for _, c := range comments {
			out = append(out, s.tagsIn(path, n, c)...)
		}
	}
	return out
}

// commentsOf is the commented parts of a line, given the block comment open at its start, and the one open at its
// end; -1 is none.
func commentsOf(line string, st style, open int) ([]string, int) {
	var out []string
	rest := line
	for rest != "" {
		if open >= 0 {
			end := strings.Index(rest, st.blocks[open][1])
			if end < 0 {
				return append(out, rest), open
			}
			out = append(out, rest[:end])
			rest, open = rest[end+len(st.blocks[open][1]):], -1
			continue
		}
		at, kind, block := len(rest), -1, false
		for _, l := range st.line {
			if i := strings.Index(rest, l); i >= 0 && i < at {
				at, kind, block = i, 0, false
			}
		}
		for b, pair := range st.blocks {
			if i := strings.Index(rest, pair[0]); i >= 0 && i < at {
				at, kind, block = i, b, true
			}
		}
		if kind < 0 {
			return out, -1
		}
		if !block {
			return append(out, rest[at:]), -1
		}
		rest, open = rest[at+len(st.blocks[kind][0]):], kind
	}
	return out, open
}

func (s *Scanner) tagsIn(path string, line int, comment string) []Found {
	var out []Found
	for _, m := range s.tag.FindAllStringSubmatch(comment, -1) {
		for _, k := range s.keys.FindAllStringSubmatch(m[2], -1) {
			n, _ := strconv.Atoi(k[2])
			tag := Found{Path: path, Line: line, Word: m[1], Key: strings.ToUpper(k[1]) + "-R" + strconv.Itoa(n)}
			if k[3] != "" {
				if rev, err := strconv.Atoi(k[3]); err == nil {
					tag.Revision = &rev
				}
			}
			out = append(out, tag)
		}
	}
	return out
}

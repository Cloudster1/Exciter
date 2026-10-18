package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudster1/exciter/backend/internal/codescan"
)

// skipped are directories a scan never reads when the checkout is not a git repository, whose ignore rules it
// would follow otherwise.
var skipped = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "target": true}

type tagWords struct {
	Implements  []string `json:"implements"`
	Verifies    []string `json:"verifies"`
	ProjectKeys []string `json:"projectKeys"`
}

type scanReported struct {
	codescan.Found
	Problem string `json:"problem"`
}

type codeScan struct {
	ID         string         `json:"id"`
	Tags       int            `json:"tags"`
	Linked     int            `json:"linked"`
	Reported   []scanReported `json:"reported"`
	Unreported int            `json:"unreported"`
}

// patterns is a flag given more than once.
type patterns []string

func (p *patterns) String() string     { return strings.Join(*p, ",") }
func (p *patterns) Set(v string) error { *p = append(*p, v); return nil }

func scan(ctx context.Context, args []string, env func(string) string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(out)
	s := common(fs, env)
	w := detect(env)
	fs.StringVar(&w.repository, "repository", w.repository, "the repository, such as acme/checkout")
	fs.StringVar(&w.commit, "commit", w.commit, "the commit scanned")
	fs.StringVar(&w.branch, "branch", w.branch, "the branch the commit is on")
	fileURL := fs.String("file-url", fileURLFrom(env), "the address of a line, with {commit}, {path} and {line}; worked out for GitHub, GitLab and Gitea")
	var exclude patterns
	fs.Var(&exclude, "exclude", "a path pattern to leave out, such as testdata/*; more than once for more")
	if err := fs.Parse(args); err != nil {
		return exitBroken, err
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	c, err := s.client()
	if err != nil {
		return exitBroken, err
	}
	fromGit(ctx, root, &w, fileURL)
	var words struct {
		Words tagWords `json:"words"`
	}
	if err := c.do(ctx, "GET", "/projects/"+url.PathEscape(s.project)+"/code-tag-words", "", nil, &words); err != nil {
		return exitBroken, err
	}
	files, err := sources(ctx, root, exclude)
	if err != nil {
		return exitBroken, err
	}
	scanner := codescan.NewScanner(append(append([]string{}, words.Words.Implements...), words.Words.Verifies...), words.Words.ProjectKeys)
	// A directory inside a checkout names its files from the checkout's top, as the repository's addresses do.
	prefix := gitOutput(ctx, root, "rev-parse", "--show-prefix")
	found := []codescan.Found{}
	for _, path := range files {
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			continue
		}
		found = append(found, scanner.File(prefix+filepath.ToSlash(path), content)...)
	}
	body, err := json.Marshal(map[string]any{"repository": w.repository, "commit": w.commit, "branch": w.branch, "fileUrl": *fileURL, "tags": found})
	if err != nil {
		return exitBroken, err
	}
	var made struct {
		Scan codeScan `json:"scan"`
	}
	if err := c.do(ctx, "POST", "/projects/"+url.PathEscape(s.project)+"/code-scans", "application/json", bytes.NewReader(body), &made); err != nil {
		return exitBroken, err
	}
	fmt.Fprintf(out, "Scanned %d files of %s on %s: %d tags, %d link a requirement.\n", len(files), w.repository, w.branch, made.Scan.Tags, made.Scan.Linked)
	for _, r := range made.Scan.Reported {
		fmt.Fprintf(out, "  %s:%d [%s %s] links nothing (%s)\n", r.Path, r.Line, r.Word, r.Key, r.Problem)
	}
	if made.Scan.Unreported > 0 {
		fmt.Fprintf(out, "  and %d more tags that link nothing\n", made.Scan.Unreported)
	}
	return exitPass, nil
}

// sources are the files a scan reads, relative to root: git's when root is a checkout, so its ignore rules hold,
// every file but the usual build output otherwise; the patterns given are left out.
func sources(ctx context.Context, root string, exclude []string) ([]string, error) {
	var files []string
	if listed, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output(); err == nil {
		for _, f := range strings.Split(string(listed), "\x00") {
			if f != "" {
				files = append(files, f)
			}
		}
	} else {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && skipped[d.Name()] {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	kept := files[:0]
	for _, f := range files {
		if !excluded(filepath.ToSlash(f), exclude) {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

func excluded(path string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, path); ok {
			return true
		}
		if ok, _ := filepath.Match(p, filepath.Base(path)); ok {
			return true
		}
		if strings.HasPrefix(path, strings.TrimSuffix(p, "/*")+"/") && strings.HasSuffix(p, "/*") {
			return true
		}
	}
	return false
}

// fileURLFrom is the address of a line in the forge a pipeline runs in, empty outside one.
func fileURLFrom(env func(string) string) string {
	switch {
	case env("GITLAB_CI") != "" && env("CI_PROJECT_URL") != "":
		return strings.TrimRight(env("CI_PROJECT_URL"), "/") + "/-/blob/{commit}/{path}#L{line}"
	case env("GITEA_ACTIONS") != "" && env("GITHUB_SERVER_URL") != "" && env("GITHUB_REPOSITORY") != "":
		return strings.TrimRight(env("GITHUB_SERVER_URL"), "/") + "/" + env("GITHUB_REPOSITORY") + "/src/commit/{commit}/{path}#L{line}"
	case env("GITHUB_ACTIONS") != "" && env("GITHUB_SERVER_URL") != "" && env("GITHUB_REPOSITORY") != "":
		return strings.TrimRight(env("GITHUB_SERVER_URL"), "/") + "/" + env("GITHUB_REPOSITORY") + "/blob/{commit}/{path}#L{line}"
	}
	return ""
}

// gitOutput is what git answers in root, empty when root is no checkout or git is not there.
func gitOutput(ctx context.Context, root string, args ...string) string {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fromGit fills what the pipeline left out from the checkout itself: its commit, branch, and the repository and the
// address of a line from its origin remote.
func fromGit(ctx context.Context, root string, w *where, fileURL *string) {
	git := func(args ...string) string { return gitOutput(ctx, root, args...) }
	if w.commit == "" {
		w.commit = git("rev-parse", "HEAD")
	}
	if w.branch == "" {
		w.branch = git("rev-parse", "--abbrev-ref", "HEAD")
	}
	base, repo, forge, err := remote(git("config", "--get", "remote.origin.url"))
	if err != nil {
		return
	}
	if w.repository == "" {
		w.repository = repo
	}
	if *fileURL == "" {
		switch forge {
		case "github":
			*fileURL = base + "/blob/{commit}/{path}#L{line}"
		case "gitlab":
			*fileURL = base + "/-/blob/{commit}/{path}#L{line}"
		case "gitea":
			*fileURL = base + "/src/commit/{commit}/{path}#L{line}"
		}
	}
}

// remote reads a git remote, ssh or https, as the repository's web address, its path, and the forge it is on when
// the host says so.
func remote(raw string) (base, repo, forge string, err error) {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if raw == "" {
		return "", "", "", errors.New("no remote")
	}
	if !strings.Contains(raw, "://") {
		// scp-like: git@host:owner/repo
		if at := strings.Index(raw, "@"); at >= 0 {
			raw = raw[at+1:]
		}
		raw = "https://" + strings.Replace(raw, ":", "/", 1)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", "", errors.New("not a remote")
	}
	repo = strings.Trim(u.Path, "/")
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "github.com" || strings.Contains(host, "github"):
		forge = "github"
	case strings.Contains(host, "gitlab"):
		forge = "gitlab"
	case strings.Contains(host, "gitea") || strings.Contains(host, "codeberg") || strings.Contains(host, "forgejo"):
		forge = "gitea"
	}
	return "https://" + u.Host + "/" + repo, repo, forge, nil
}

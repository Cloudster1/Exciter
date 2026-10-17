package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultWait is how long results waits for a run to land unless told otherwise.
const defaultWait = 10 * time.Minute

// pollEvery is how often results looks at a run while it waits; a test looks more often.
var pollEvery = 2 * time.Second

// common are the flags every command takes, over the environment.
func common(fs *flag.FlagSet, env func(string) string) *settings {
	s := &settings{url: env("EXCITER_URL"), token: env("EXCITER_TOKEN"), project: env("EXCITER_PROJECT")}
	fs.StringVar(&s.url, "url", s.url, "Exciter's address; EXCITER_URL")
	fs.StringVar(&s.project, "project", s.project, "the project's key; EXCITER_PROJECT")
	return s
}

type reported struct {
	Key     string `json:"key"`
	Test    string `json:"test"`
	Problem string `json:"problem"`
}

type testRun struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Cases  int    `json:"cases"`
	Match  int    `json:"matched"`
	Counts struct {
		Passed, Failed, Skipped, Errored int
	} `json:"counts"`
	Reported   []reported `json:"reported"`
	Unreported int        `json:"unreported"`
	Error      string     `json:"error"`
}

func results(ctx context.Context, args []string, env func(string) string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("results", flag.ContinueOnError)
	fs.SetOutput(out)
	s := common(fs, env)
	w := detect(env)
	fs.StringVar(&w.repository, "repository", w.repository, "the repository, such as acme/checkout")
	fs.StringVar(&w.commit, "commit", w.commit, "the commit the tests ran on")
	fs.StringVar(&w.branch, "branch", w.branch, "the branch the commit is on")
	fs.StringVar(&w.job, "job", w.job, "tells apart the runs of one commit, such as unit or browser")
	fs.StringVar(&w.runURL, "run-url", w.runURL, "the CI run's address")
	wait := fs.Duration("wait", defaultWait, "how long to wait for the run to land; 0 does not wait")
	if err := fs.Parse(args); err != nil {
		return exitBroken, err
	}
	c, err := s.client()
	if err != nil {
		return exitBroken, err
	}
	files, err := reports(fs.Args())
	if err != nil {
		return exitBroken, err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{"repository": w.repository, "commit": w.commit, "branch": w.branch, "job": w.job, "runUrl": w.runURL} {
		if value != "" {
			_ = form.WriteField(name, value)
		}
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return exitBroken, err
		}
		part, err := form.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			return exitBroken, err
		}
		_, _ = part.Write(data)
	}
	_ = form.Close()
	var made struct {
		Run testRun `json:"run"`
	}
	if err := c.do(ctx, "POST", "/projects/"+url.PathEscape(s.project)+"/test-runs", form.FormDataContentType(), &body, &made); err != nil {
		return exitBroken, err
	}
	fmt.Fprintf(out, "Uploaded %d report(s) to %s as run %s.\n", len(files), s.project, made.Run.ID)
	if *wait <= 0 {
		return exitPass, nil
	}
	r, err := waitFor(ctx, c, made.Run.ID, *wait)
	if err != nil {
		return exitBroken, err
	}
	if r.State == "failed" {
		return exitBroken, errors.New(r.Error)
	}
	fmt.Fprintf(out, "%d test cases read, %d name a requirement: %d passed, %d failed, %d errored, %d skipped.\n",
		r.Cases, r.Match, r.Counts.Passed, r.Counts.Failed, r.Counts.Errored, r.Counts.Skipped)
	for _, k := range r.Reported {
		fmt.Fprintf(out, "  %s verifies nothing (%s), named by %s\n", k.Key, k.Problem, k.Test)
	}
	if r.Unreported > 0 {
		fmt.Fprintf(out, "  and %d more keys that verify nothing\n", r.Unreported)
	}
	return exitPass, nil
}

// reports expands the files and patterns given, a pattern quoted against the shell included.
func reports(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		matches, err := filepath.Glob(a)
		if err != nil {
			return nil, fmt.Errorf("%q is not a pattern: %w", a, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no report matches %s", a)
		}
		out = append(out, matches...)
	}
	if len(out) == 0 {
		return nil, errors.New("name the JUnit XML reports to upload")
	}
	return out, nil
}

// waitFor reads a run until the worker has landed it.
func waitFor(ctx context.Context, c *client, id string, limit time.Duration) (*testRun, error) {
	deadline := time.Now().Add(limit)
	for {
		var got struct {
			Run testRun `json:"run"`
		}
		if err := c.do(ctx, "GET", "/test-runs/"+url.PathEscape(id), "", nil, &got); err != nil {
			return nil, err
		}
		if got.Run.State == "done" || got.Run.State == "failed" {
			return &got.Run, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the run is still %s after %s; read it later at /test-runs/%s", got.Run.State, limit, id)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

type checkRef struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

type traceCheck struct {
	Verdict string   `json:"verdict"`
	Branch  string   `json:"branch"`
	Fail    []string `json:"fail"`
	Chains  bool     `json:"chains"`
	Broken  []struct {
		Root    checkRef `json:"root"`
		Path    []string `json:"path"`
		Missing string   `json:"missing"`
	} `json:"broken"`
	Gaps []struct {
		Requirement checkRef `json:"requirement"`
		Dimension   string   `json:"dimension"`
		Level       string   `json:"level"`
	} `json:"gaps"`
	Failing  []evidence `json:"failing"`
	Outdated []evidence `json:"outdated"`
	Orphaned []struct {
		Key, Problem, Repository, Path string
		Line                           int
	} `json:"orphaned"`
	Totals   map[string]int `json:"totals"`
	Evidence struct {
		Runs, Scans int
	} `json:"evidence"`
	Checked int `json:"checked"`
}

type evidence struct {
	Requirement checkRef `json:"requirement"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Outcome     string   `json:"outcome"`
	Revision    int      `json:"revision"`
}

func check(ctx context.Context, args []string, env func(string) string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(out)
	s := common(fs, env)
	branch := fs.String("branch", detect(env).branch, "the branch whose evidence counts; the project's counting branch when empty")
	query := fs.String("query", "", "a query in Exciter's query language that keeps the requirements to check")
	fail := fs.String("fail", "", "what fails the check, of broken, gaps, failing, outdated and orphaned; broken,gaps,failing when empty")
	if err := fs.Parse(args); err != nil {
		return exitBroken, err
	}
	c, err := s.client()
	if err != nil {
		return exitBroken, err
	}
	q := url.Values{}
	for name, value := range map[string]string{"branch": *branch, "query": *query, "fail": *fail} {
		if value != "" {
			q.Set(name, value)
		}
	}
	var got struct {
		Check traceCheck `json:"check"`
	}
	if err := c.do(ctx, "GET", "/projects/"+url.PathEscape(s.project)+"/trace-check?"+q.Encode(), "", nil, &got); err != nil {
		return exitBroken, err
	}
	report(out, got.Check)
	if got.Check.Verdict == "pass" {
		return exitPass, nil
	}
	return exitFail, nil
}

// report prints a check as a pipeline's log reads it: the verdict, then what was found.
func report(out io.Writer, c traceCheck) {
	noun := "requirements"
	if c.Checked == 1 {
		noun = "requirement"
	}
	fmt.Fprintf(out, "Trace check of %d %s on %s: %s, failing on %s.\n", c.Checked, noun, c.Branch, strings.ToUpper(c.Verdict), strings.Join(c.Fail, ", "))
	if c.Evidence.Runs == 0 {
		fmt.Fprintf(out, "No test run was uploaded on %s yet.\n", c.Branch)
	}
	if !c.Chains {
		fmt.Fprintln(out, "The project has no trace model, so no chain was followed.")
	}
	for _, b := range c.Broken {
		fmt.Fprintf(out, "  broken  %s stops at %s, reaching no %s\n", b.Root.Key, strings.Join(b.Path, " > "), b.Missing)
	}
	for _, g := range c.Gaps {
		fmt.Fprintf(out, "  gap     %s is %s in %s\n", g.Requirement.Key, g.Level, g.Dimension)
	}
	for _, e := range c.Failing {
		fmt.Fprintf(out, "  failing %s: %s %s\n", e.Requirement.Key, e.Name, e.Outcome)
	}
	for _, e := range c.Outdated {
		fmt.Fprintf(out, "  outdated %s: %s was made against revision %d\n", e.Requirement.Key, e.Name, e.Revision)
	}
	for _, o := range c.Orphaned {
		fmt.Fprintf(out, "  orphaned %s at %s:%d (%s)\n", o.Key, o.Path, o.Line, o.Problem)
	}
	for _, name := range []string{"broken", "gaps", "failing", "outdated", "orphaned"} {
		if n, shown := c.Totals[name], listed(c, name); n > shown {
			fmt.Fprintf(out, "  and %d more %s\n", n-shown, name)
		}
	}
}

func listed(c traceCheck, name string) int {
	switch name {
	case "broken":
		return len(c.Broken)
	case "gaps":
		return len(c.Gaps)
	case "failing":
		return len(c.Failing)
	case "outdated":
		return len(c.Outdated)
	}
	return len(c.Orphaned)
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// requestTimeout bounds one call to Exciter; a whole wait is bounded by --wait.
const requestTimeout = 60 * time.Second

// client calls Exciter's API with a token.
type client struct {
	base  string
	token string
	http  *http.Client
}

// settings are what every command reads from its environment, flags overriding them.
type settings struct {
	url, token, project string
}

func (s settings) client() (*client, error) {
	switch {
	case s.url == "":
		return nil, errors.New("set EXCITER_URL, or --url, to the address Exciter is served at")
	case s.token == "":
		return nil, errors.New("set EXCITER_TOKEN to a token with the evidence scope")
	case s.project == "":
		return nil, errors.New("set EXCITER_PROJECT, or --project, to the project's key")
	}
	u, err := url.Parse(s.url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("EXCITER_URL %q is not an http or https address", s.url)
	}
	return &client{base: strings.TrimRight(s.url, "/") + "/api/v1", token: s.token, http: &http.Client{Timeout: requestTimeout}}, nil
}

// refusal is Exciter's answer to a request it refused: its sentence, and what it says of each field.
type refusal struct {
	Status  int
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields"`
}

func (r *refusal) Error() string {
	msg := r.Message
	if msg == "" {
		msg = http.StatusText(r.Status)
	}
	names := make([]string, 0, len(r.Fields))
	for name := range r.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		msg += " " + name + ": " + r.Fields[name]
	}
	return msg
}

// do sends a request and decodes the answer into out, or answers the refusal.
func (c *client) do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Exciter could not be reached: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		var envelope struct {
			Error refusal `json:"error"`
		}
		_ = json.Unmarshal(raw, &envelope)
		envelope.Error.Status = resp.StatusCode
		return &envelope.Error
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(out); err != nil {
		return fmt.Errorf("Exciter answered what exciter-ci does not read: %w", err)
	}
	return nil
}

//go:build integration

package test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/cloudster1/exciter/backend/internal/objectstore"
	"github.com/cloudster1/exciter/backend/internal/theme"
)

// bucket is the suite's own bucket in the stack's object store, made when it is missing.
func (h *harness) bucket(t *testing.T) objectstore.Store {
	t.Helper()
	if os.Getenv("EXCITER_S3_ENDPOINT") == "" {
		t.Fatal("EXCITER_S3_ENDPOINT is not set; run the suite with make test-integration against the running stack")
	}
	store, err := objectstore.Open(objectstore.Config{
		Endpoint: h.cfg.S3.Endpoint, Bucket: h.cfg.S3.Bucket, AccessKey: h.cfg.S3.AccessKey,
		SecretKey: h.cfg.S3.SecretKey, Region: h.cfg.S3.Region, UseSSL: h.cfg.S3.UseSSL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.(*objectstore.S3Store).EnsureBucket(context.Background()); err != nil {
		t.Fatalf("make the bucket: %v", err)
	}
	return store
}

// upload sends a file as a multipart part named file, the way the web does.
func (c *client) upload(t *testing.T, path, name string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, c.api.srv.URL+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	return c.do(t, req)
}

// download fetches a file and keeps the headers, which say how it is served.
func (c *client) download(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.api.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := c.do(t, req)
	return &http.Response{StatusCode: got.Status, Header: got.Header}, got.Raw
}

// do sends a request already made, as call does with the JSON it makes.
func (c *client) do(t *testing.T, req *http.Request) response {
	t.Helper()
	c.api.handOver(t, c, req.Method)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return decoded(resp, raw)
}

func themeObject(org uuid.UUID, themeID, assetID string) string {
	return theme.ObjectKey(org, uuid.MustParse(themeID), uuid.MustParse(assetID))
}

func ptr[T any](v T) *T { return &v }

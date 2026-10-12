package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The GET Object example from the S3 signature documentation, which pins the
// canonicalisation and the key derivation to a published answer.
func TestSignatureMatchesTheDocumentedExample(t *testing.T) {
	headers := map[string]string{
		"host":                 "examplebucket.s3.amazonaws.com",
		"range":                "bytes=0-9",
		"x-amz-content-sha256": emptyPayloadHash,
		"x-amz-date":           "20130524T000000Z",
	}
	got := authorization("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1",
		http.MethodGet, "/test.txt", "", headers, emptyPayloadHash)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got != want {
		t.Fatalf("authorization\n got %s\nwant %s", got, want)
	}
}

func TestKeysAreEncodedSegmentBySegment(t *testing.T) {
	got := encodePath("org/abc/issue/x y/notes (final).md")
	want := "org/abc/issue/x%20y/notes%20%28final%29.md"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if encodeSegment("\u00fc") != "%C3%BC" {
		t.Fatalf("multi byte characters are encoded byte by byte, got %s", encodeSegment("\u00fc"))
	}
}

// fakeS3 is enough of a bucket to see the requests the store makes: paths,
// signatures present, bodies stored and returned.
type fakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
	seen    []string
	// ignoreRanges answers every read whole, as some stores do.
	ignoreRanges bool
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, r.Method+" "+r.URL.EscapedPath())
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") ||
		r.Header.Get("X-Amz-Content-Sha256") == "" || r.Header.Get("X-Amz-Date") == "" {
		http.Error(w, "<Error><Code>AccessDenied</Code><Message>unsigned</Message></Error>", http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != f.bucket {
		http.Error(w, "<Error><Code>NoSuchBucket</Code><Message>no</Message></Error>", http.StatusNotFound)
		return
	}
	switch {
	case key == "" && r.Method == http.MethodHead:
		if f.objects == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodPut:
		f.objects = map[string][]byte{}
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodGet:
		f.list(w, r)
	case r.Method == http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		f.objects[key] = data
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code><Message>gone</Message></Error>", http.StatusNotFound)
			return
		}
		if want := r.Header.Get("Range"); want != "" && !f.ignoreRanges {
			if !strings.Contains(r.Header.Get("Authorization"), "range;") {
				http.Error(w, "<Error><Code>AccessDenied</Code><Message>range unsigned</Message></Error>", http.StatusForbidden)
				return
			}
			var from, to int
			if _, err := fmt.Sscanf(want, "bytes=%d-%d", &from, &to); err != nil || to >= len(data) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[from : to+1])
			return
		}
		_, _ = w.Write(data)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// fakeListPage is how many keys the fake lists at once, small so a test
// walks more than one page.
const fakeListPage = 2

// list answers ListObjectsV2, a page at a time, the continuation token being
// the last key of the page before.
func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("list-type") != "2" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, q.Get("prefix")) && key > q.Get("continuation-token") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	truncated := len(keys) > fakeListPage
	if truncated {
		keys = keys[:fakeListPage]
	}
	var b strings.Builder
	b.WriteString("<ListBucketResult>")
	for _, key := range keys {
		b.WriteString("<Contents><Key>" + key + "</Key></Contents>")
	}
	if truncated {
		b.WriteString("<IsTruncated>true</IsTruncated><NextContinuationToken>" + keys[len(keys)-1] + "</NextContinuationToken>")
	}
	b.WriteString("</ListBucketResult>")
	_, _ = w.Write([]byte(b.String()))
}

func TestDeletePrefixTakesEveryPageAndNothingElse(t *testing.T) {
	fake := &fakeS3{bucket: "att"}
	server := httptest.NewServer(fake)
	defer server.Close()
	store, err := NewS3(Config{Endpoint: server.URL, Bucket: "att", AccessKey: "k", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	gone, kept := "org/a/", "org/ab/"
	for _, key := range []string{gone + "1", gone + "2", gone + "theme/x/3", gone + "theme/y/4", gone + "5", kept + "1"} {
		if err := store.Put(ctx, key, strings.NewReader("x"), 1, "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeletePrefix(ctx, store, gone); err != nil {
		t.Fatalf("delete the prefix: %v", err)
	}
	if left, _ := store.List(ctx, "org/"); strings.Join(left, ",") != kept+"1" {
		t.Fatalf("left behind %v, want only %s1", left, kept)
	}
	if err := DeletePrefix(ctx, store, ""); err == nil {
		t.Fatal("an empty prefix emptied the bucket")
	}
}

func TestS3StoreAgainstAFakeBucket(t *testing.T) {
	fake := &fakeS3{bucket: "att"}
	server := httptest.NewServer(fake)
	defer server.Close()

	store, err := NewS3(Config{Endpoint: server.URL, Bucket: "att", AccessKey: "k", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket again: %v", err)
	}
	key := "org/1/issue/2/3/notes (final).md"
	if err := store.Put(ctx, key, strings.NewReader("hello"), 5, "text/markdown"); err != nil {
		t.Fatalf("put: %v", err)
	}
	body, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "hello" {
		t.Fatalf("got %q", data)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, key); err != ErrNoObject {
		t.Fatalf("want ErrNoObject after delete, got %v", err)
	}

	want := []string{"HEAD /att", "PUT /att", "HEAD /att",
		"PUT /att/org/1/issue/2/3/notes%20%28final%29.md",
		"GET /att/org/1/issue/2/3/notes%20%28final%29.md",
		"DELETE /att/org/1/issue/2/3/notes%20%28final%29.md",
		"GET /att/org/1/issue/2/3/notes%20%28final%29.md"}
	if strings.Join(fake.seen, "|") != strings.Join(want, "|") {
		t.Fatalf("requests\n got %v\nwant %v", fake.seen, want)
	}
}

func TestEndpointForms(t *testing.T) {
	for _, in := range []string{"seaweedfs:8333", "http://seaweedfs:8333/", "https://s3.eu-central-1.amazonaws.com"} {
		if _, err := NewS3(Config{Endpoint: in, Bucket: "b"}); err != nil {
			t.Errorf("%q should be accepted: %v", in, err)
		}
	}
	if _, err := NewS3(Config{Endpoint: "seaweedfs:8333"}); err == nil {
		t.Error("a missing bucket should be refused")
	}
}

func TestARangeIsAskedOfTheBucketAndCutWhenItIgnoresIt(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		fake := &fakeS3{bucket: "att", ignoreRanges: ignore}
		server := httptest.NewServer(fake)
		store, err := NewS3(Config{Endpoint: server.URL, Bucket: "att", AccessKey: "k", SecretKey: "s"})
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if err := store.EnsureBucket(ctx); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(ctx, "clip.mp4", strings.NewReader("0123456789"), 10, "video/mp4"); err != nil {
			t.Fatal(err)
		}
		body, err := store.GetRange(ctx, "clip.mp4", 3, 4)
		if err != nil {
			t.Fatalf("ignoring ranges %v: %v", ignore, err)
		}
		data, _ := io.ReadAll(body)
		body.Close()
		if string(data) != "3456" {
			t.Errorf("ignoring ranges %v: got %q, want 3456", ignore, data)
		}
		if _, err := store.GetRange(ctx, "gone.mp4", 0, 1); err != ErrNoObject {
			t.Errorf("a missing object: %v", err)
		}
		server.Close()
	}
}

func TestTheMemoryStoreReadsARangeAndRefusesOneOutside(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	_ = m.Put(ctx, "k", strings.NewReader("abcdef"), 6, "text/plain")
	body, err := m.GetRange(ctx, "k", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := io.ReadAll(body); string(data) != "cde" {
		t.Fatalf("got %q", data)
	}
	if _, err := m.GetRange(ctx, "k", 4, 3); err == nil {
		t.Fatal("a range past the end was read")
	}
}

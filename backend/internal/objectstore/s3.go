package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3Store keeps objects in one bucket of any S3 compatible service, with the
// standard library alone: seven signed calls, rather than a dozen modules.
type S3Store struct {
	endpoint  *url.URL
	bucket    string
	region    string
	accessKey string
	secretKey string
	client    *http.Client
	now       func() time.Time
}

// clientTimeout bounds one call to the bucket, long enough for the largest
// upload; errorBodyLimit is how much of a refusal is read for its reason.
const (
	clientTimeout  = 5 * time.Minute
	errorBodyLimit = 64 << 10
)

// emptyPayloadHash is sha256 of nothing, which every bodiless request carries.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// NewS3 prepares a client. It does not touch the network; EnsureBucket does.
func NewS3(cfg Config) (*S3Store, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("s3: a bucket name is required")
	}
	raw := strings.TrimSuffix(strings.TrimSpace(cfg.Endpoint), "/")
	if !strings.Contains(raw, "://") {
		scheme := "http"
		if cfg.UseSSL {
			scheme = "https"
		}
		raw = scheme + "://" + raw
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" {
		return nil, fmt.Errorf("s3: %q is not an endpoint such as seaweedfs:8333 or https://s3.eu-central-1.amazonaws.com", cfg.Endpoint)
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	return &S3Store{
		endpoint:  endpoint,
		bucket:    cfg.Bucket,
		region:    region,
		accessKey: cfg.AccessKey,
		secretKey: cfg.SecretKey,
		client:    &http.Client{Timeout: clientTimeout},
		now:       time.Now,
	}, nil
}

// EnsureBucket makes the bucket if it is not there, which is what a fresh
// development stack needs and what production has already done by hand.
func (s *S3Store) EnsureBucket(ctx context.Context) error {
	resp, err := s.do(ctx, http.MethodHead, "", nil, 0, emptyPayloadHash, "")
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", s.bucket, err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
	default:
		return fmt.Errorf("check bucket %s: %s", s.bucket, resp.Status)
	}

	var body []byte
	if s.region != "us-east-1" {
		body = []byte(`<CreateBucketConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><LocationConstraint>` +
			s.region + `</LocationConstraint></CreateBucketConfiguration>`)
	}
	resp, err = s.do(ctx, http.MethodPut, "", bytes.NewReader(body), int64(len(body)), hashOf(body), "")
	if err != nil {
		return fmt.Errorf("make bucket %s: %w", s.bucket, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	code, message := s3Error(resp)
	// Two processes starting at once both find it missing; the loser is fine.
	if code == "BucketAlreadyOwnedByYou" || code == "BucketAlreadyExists" {
		return nil
	}
	return fmt.Errorf("make bucket %s: %s %s", s.bucket, code, message)
}

// Put stores one object. The signature needs the body's hash first, so a body
// that can rewind is hashed then streamed, and anything else read into memory.
func (s *S3Store) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	seeker, ok := body.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("store object: %w", err)
		}
		seeker = bytes.NewReader(data)
		size = int64(len(data))
	}
	hash := sha256.New()
	hashed, err := io.Copy(hash, seeker)
	if err != nil {
		return fmt.Errorf("store object: %w", err)
	}
	if size >= 0 && hashed != size {
		return fmt.Errorf("store object: expected %d bytes, read %d", size, hashed)
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("store object: %w", err)
	}
	resp, err := s.do(ctx, http.MethodPut, key, seeker, hashed, hex.EncodeToString(hash.Sum(nil)), contentType)
	if err != nil {
		return fmt.Errorf("store object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		code, message := s3Error(resp)
		return fmt.Errorf("store object: %s %s", code, message)
	}
	return nil
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := s.do(ctx, http.MethodGet, key, nil, 0, emptyPayloadHash, "")
	if err != nil {
		return nil, fmt.Errorf("fetch object: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp.Body, nil
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNoObject
	default:
		defer resp.Body.Close()
		code, message := s3Error(resp)
		return nil, fmt.Errorf("fetch object: %s %s", code, message)
	}
}

// GetRange asks the bucket for the stretch alone, so a reader seeking in a
// long video costs the bytes it plays rather than the whole file.
func (s *S3Store) GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length <= 0 {
		return nil, fmt.Errorf("fetch object: no bytes in %d from %d", length, offset)
	}
	extra := map[string]string{"range": fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)}
	resp, err := s.send(ctx, http.MethodGet, key, "", nil, 0, emptyPayloadHash, "", extra)
	if err != nil {
		return nil, fmt.Errorf("fetch object: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusPartialContent:
		return resp.Body, nil
	case http.StatusOK:
		// A store that ignores the range sends everything; the stretch is cut here.
		if _, err := io.CopyN(io.Discard, resp.Body, offset); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("fetch object: %w", err)
		}
		return limitedBody{Reader: io.LimitReader(resp.Body, length), Closer: resp.Body}, nil
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNoObject
	default:
		defer resp.Body.Close()
		code, message := s3Error(resp)
		return nil, fmt.Errorf("fetch object: %s %s", code, message)
	}
}

type limitedBody struct {
	io.Reader
	io.Closer
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	resp, err := s.do(ctx, http.MethodDelete, key, nil, 0, emptyPayloadHash, "")
	if err != nil {
		return fmt.Errorf("remove object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	code, message := s3Error(resp)
	return fmt.Errorf("remove object: %s %s", code, message)
}

// List names every object whose key starts with prefix, following the
// listing page by page.
func (s *S3Store) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	token := ""
	for {
		params := map[string]string{"list-type": "2", "prefix": prefix}
		if token != "" {
			params["continuation-token"] = token
		}
		resp, err := s.doQuery(ctx, http.MethodGet, "", canonicalQuery(params), nil, 0, emptyPayloadHash, "")
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			code, message := s3Error(resp)
			resp.Body.Close()
			return nil, fmt.Errorf("list objects: %s %s", code, message)
		}
		var page struct {
			Contents []struct {
				Key string `xml:"Key"`
			} `xml:"Contents"`
			IsTruncated           bool   `xml:"IsTruncated"`
			NextContinuationToken string `xml:"NextContinuationToken"`
		}
		err = xml.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}
		for _, c := range page.Contents {
			keys = append(keys, c.Key)
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			return keys, nil
		}
		token = page.NextContinuationToken
	}
}

func canonicalQuery(params map[string]string) string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = encodeSegment(name) + "=" + encodeSegment(params[name])
	}
	return strings.Join(parts, "&")
}

// hashOf is the payload hash of a small body held in memory.
func hashOf(body []byte) string {
	if len(body) == 0 {
		return emptyPayloadHash
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// do sends one signed, path style request (host/bucket/key), which every S3
// compatible service accepts; the caller, who has seen the body, hashes it.
func (s *S3Store) do(ctx context.Context, method, key string, body io.Reader, size int64, payloadHash, contentType string) (*http.Response, error) {
	return s.doQuery(ctx, method, key, "", body, size, payloadHash, contentType)
}

// doQuery is do with a query string already in canonical form: names sorted,
// each name and value encoded as encodeSegment does.
func (s *S3Store) doQuery(ctx context.Context, method, key, query string, body io.Reader, size int64, payloadHash, contentType string) (*http.Response, error) {
	return s.send(ctx, method, key, query, body, size, payloadHash, contentType, nil)
}

// send is doQuery with further headers, named in lower case, signed with the rest.
func (s *S3Store) send(ctx context.Context, method, key, query string, body io.Reader, size int64, payloadHash, contentType string, extra map[string]string) (*http.Response, error) {
	path := "/" + encodeSegment(s.bucket)
	if key != "" {
		path += "/" + encodePath(key)
	}
	target := *s.endpoint
	target.Path = "/" + s.bucket
	target.RawPath = path
	if key != "" {
		target.Path += "/" + key
	}
	target.RawQuery = query

	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = size

	headers := map[string]string{
		"host":                 req.URL.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           s.now().UTC().Format("20060102T150405Z"),
	}
	if contentType != "" {
		headers["content-type"] = contentType
	}
	for name, value := range extra {
		headers[name] = value
	}
	for name, value := range headers {
		if name != "host" {
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Authorization", authorization(s.accessKey, s.secretKey, s.region, method, path, query, headers, payloadHash))
	return s.client.Do(req)
}

// authorization computes the Signature Version 4 header for one request, dated
// by x-amz-date so a test can pin it to the published example.
func authorization(accessKey, secretKey, region, method, canonicalPath, canonicalQuery string, headers map[string]string, payloadHash string) string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)

	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.Join(strings.Fields(headers[name]), " "))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		method, canonicalPath, canonicalQuery, canonicalHeaders.String(), signedHeaders, payloadHash,
	}, "\n")

	amzDate := headers["x-amz-date"]
	date := amzDate[:8]
	scope := date + "/" + region + "/s3/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, hex.EncodeToString(requestHash[:])}, "\n")

	signingKey := hmacSHA256([]byte("AWS4"+secretKey), date)
	signingKey = hmacSHA256(signingKey, region)
	signingKey = hmacSHA256(signingKey, "s3")
	signingKey = hmacSHA256(signingKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	return "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

// encodePath percent-encodes an object key the way S3 canonicalises it: each
// segment on its own, the slashes between them kept.
func encodePath(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = encodeSegment(segment)
	}
	return strings.Join(segments, "/")
}

// encodeSegment keeps the unreserved characters and encodes every other byte,
// upper case, which is the one form the signature and the server agree on.
func encodeSegment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// s3Error reads the code and message out of an error response.
func s3Error(resp *http.Response) (code, message string) {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	var parsed struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if err := xml.Unmarshal(data, &parsed); err != nil || parsed.Code == "" {
		return resp.Status, strings.TrimSpace(string(data))
	}
	return parsed.Code, parsed.Message
}

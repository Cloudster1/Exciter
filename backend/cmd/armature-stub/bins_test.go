package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A bin keeps every delivery as it came and answers what it was told, so a
// suite can read a signature over the raw body and make a receiver fail.
func TestABinKeepsWhatItWasSentAndAnswersAsTold(t *testing.T) {
	server := httptest.NewServer(newStub(nil, nil).handler())
	defer server.Close()
	post := func(body string) int {
		req, _ := http.NewRequest(http.MethodPost, server.URL+hooksPrefix+"/b1", strings.NewReader(body))
		req.Header.Set("X-Exciter-Signature-256", "sha256=x")
		req.Header.Set("Authorization", "kept out")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post(`{"a":1}`); got != http.StatusNoContent {
		t.Fatalf("first delivery answered %d", got)
	}
	req, _ := http.NewRequest(http.MethodPut, server.URL+hooksPrefix+"/b1/status", strings.NewReader(`{"status":503}`))
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setting the status: %v %v", resp, err)
	}
	if got := post(`{"a":2}`); got != http.StatusServiceUnavailable {
		t.Fatalf("second delivery answered %d", got)
	}
	resp, err := http.Get(server.URL + hooksPrefix + "/b1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Deliveries []binDelivery `json:"deliveries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Deliveries) != 2 || out.Deliveries[1].Body != `{"a":2}` || out.Deliveries[0].Headers["X-Exciter-Signature-256"] != "sha256=x" {
		t.Fatalf("the bin kept %+v", out.Deliveries)
	}
	if _, kept := out.Deliveries[0].Headers["Authorization"]; kept {
		t.Error("the bin kept a credential header")
	}
}

// A bin told to redirect names where to, and one told to hold keeps the sender waiting until it gives up.
func TestABinRedirectsOrHoldsAsTold(t *testing.T) {
	server := httptest.NewServer(newStub(nil, nil).handler())
	defer server.Close()
	set := func(body string) int {
		req, _ := http.NewRequest(http.MethodPut, server.URL+hooksPrefix+"/b2/status", strings.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := set(`{"status":307,"location":"http://10.0.0.1/"}`); got != http.StatusNoContent {
		t.Fatalf("setting a redirect answered %d", got)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Post(server.URL+hooksPrefix+"/b2", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != "http://10.0.0.1/" {
		t.Fatalf("the redirect answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	const held = 2 * time.Second
	if got := set(fmt.Sprintf(`{"status":204,"holdMs":%d}`, held.Milliseconds())); got != http.StatusNoContent {
		t.Fatalf("setting a hold answered %d", got)
	}
	impatient := &http.Client{Timeout: held / 4}
	if _, err := impatient.Post(server.URL+hooksPrefix+"/b2", "application/json", strings.NewReader(`{}`)); err == nil {
		t.Fatal("a held bin answered before the sender gave up")
	}
	if got := set(fmt.Sprintf(`{"status":204,"holdMs":%d}`, (maxHold + time.Second).Milliseconds())); got != http.StatusBadRequest {
		t.Errorf("a hold past maxHold answered %d", got)
	}
}

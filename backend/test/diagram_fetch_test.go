//go:build integration

package test

import (
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// canaryWords are what the planted include would draw, had Kroki fetched it.
const canaryWords = "LeakedFromTheNetwork"

// canary serves a PlantUML include on the stack's network, where Kroki could reach it, and counts who asks.
func canary(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var asked atomic.Int32
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		_, _ = w.Write([]byte("Alice -> " + canaryWords + "\n"))
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ownAddress(t) + ":" + portOf(l.Addr()), &asked
}

// ownAddress is this container's address on the stack's network, the one Kroki would connect to.
func ownAddress(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLoopback() && ip.IP.To4() != nil {
			return ip.IP.String()
		}
	}
	t.Fatal("this container has no address but the loopback")
	return ""
}

func portOf(a net.Addr) string {
	_, port, _ := net.SplitHostPort(a.String())
	return port
}

// Kroki runs in its secure mode, so a diagram cannot make it fetch from the network: not from the cloud's metadata
// address, not from the internet, and not from a server beside it.
func TestADiagramFetchesNothingFromElsewhere(t *testing.T) {
	f := newProjectFixture(t)
	local, asked := canary(t)
	// The canary answers on that address, so silence from it means nothing asked.
	if resp, err := http.Get(local + "/control"); err != nil || resp.StatusCode != http.StatusOK || asked.Swap(0) != 1 {
		t.Fatalf("the canary at %s does not answer itself: %v", local, err)
	} else {
		resp.Body.Close()
	}
	for _, include := range []string{
		"!includeurl http://169.254.169.254/latest/meta-data/",
		"!include https://example.com/x.puml",
		"!includeurl " + local + "/x.puml",
		"!include " + local + "/y.puml",
	} {
		got := f.plain.post(t, "/api/v1/diagrams", map[string]any{"language": "plantuml", "source": "@startuml\n" + include + "\nA -> B\n@enduml"})
		switch got.Status {
		case http.StatusOK:
			svg, _ := got.Body["svg"].(string)
			if !strings.Contains(svg, "<svg") {
				t.Errorf("%s: drawn as %.200q, want a drawing", include, svg)
			}
			for _, fetched := range []string{canaryWords, "ami-id", "instance-id", "Example Domain"} {
				if strings.Contains(svg, fetched) {
					t.Errorf("%s: the drawing holds %q, which only a fetch could have brought", include, fetched)
				}
			}
		case http.StatusUnprocessableEntity:
			if refusal, _ := got.Body["error"].(map[string]any); refusal["code"] != "diagram_source" {
				t.Errorf("%s: refused as %v, want the renderer's diagram_source", include, got.Body)
			}
		default:
			t.Errorf("%s: answered %d %s, want a drawing or the renderer's refusal", include, got.Status, got.Raw)
		}
	}
	if n := asked.Load(); n != 0 {
		t.Errorf("Kroki fetched from the server beside it %d times", n)
	}
}

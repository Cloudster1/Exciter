package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// proxyChild marks the process that runs with a proxy in its environment; net/http reads that environment once per
// process, so only a process of its own sees the proxy this test names.
const proxyChild = "NETGUARD_PROXY_CHILD"

// Transport takes its proxy from HTTPS_PROXY and HTTP_PROXY, as an operator sets them, and still refuses an inner
// destination before anything reaches the proxy.
func TestTransportChecksTheDestinationOfAProxyFromTheEnvironment(t *testing.T) {
	if os.Getenv(proxyChild) == "" {
		child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
		child.Env = append(os.Environ(), proxyChild+"=1")
		out, err := child.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "--- PASS: "+t.Name()) {
			t.Fatalf("the run with a proxy in its environment failed: %v\n%s", err, out)
		}
		return
	}

	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied.Add(1)
		_, _ = w.Write([]byte("forwarded"))
	}))
	defer proxy.Close()
	for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY"} {
		t.Setenv(name, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	client := Client(2*time.Second, ParseAllow("127.0.0.1/32"))

	for _, target := range []string{"http://10.1.2.3/", "https://10.1.2.3/", "https://169.254.169.254/latest", "http://[64:ff9b::a00:1]/"} {
		if _, err := client.Get(target); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s through the environment's proxy: error = %v, want it refused", target, err)
		}
	}
	if n := proxied.Load(); n != 0 {
		t.Fatalf("the proxy was sent %d requests for reserved destinations", n)
	}
	resp, err := client.Get("http://93.184.216.34/")
	if err != nil {
		t.Fatalf("a public destination through the environment's proxy was refused: %v", err)
	}
	resp.Body.Close()
	if proxied.Load() != 1 {
		t.Fatal("a public destination did not go through the proxy the environment names")
	}
}

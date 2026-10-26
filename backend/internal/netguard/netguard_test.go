package netguard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTheServersOwnNetworkIsNotTheInternet(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "::1", "10.1.2.3", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "100.64.0.1", "0.0.0.0", "fd00::1", "224.0.0.1",
	} {
		if !Reserved(netip.MustParseAddr(address)) {
			t.Errorf("%s is inside the server's network, and was treated as the internet", address)
		}
	}
	for _, address := range []string{
		"93.184.216.34", "8.8.8.8", "2606:2800:220:1:248:1893:25c8:1946",
		"64:ff9b::808:808", "2002:808:808::1", "192.31.196.1",
	} {
		if Reserved(netip.MustParseAddr(address)) {
			t.Errorf("%s is on the internet, and was refused", address)
		}
	}
}

// An Armature address is something an administrator typed, so the one thing it
// must not reach is whatever else answers on this network.
// Each range the special-purpose registries set aside, and the translation
// prefixes judged by the IPv4 address they carry.
func TestSpecialPurposeRangesAreReserved(t *testing.T) {
	for _, address := range []string{
		"0.1.2.3", "192.0.0.8", "192.0.0.170", "192.0.2.1", "192.88.99.1",
		"198.18.0.1", "198.19.255.254", "198.51.100.1", "203.0.113.1", "240.0.0.1", "255.255.255.255",
		"::", "::1.2.3.4", "64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "64:ff9b:1::808:808",
		"100::1", "2001::1", "2001:2::1", "2001:db8::1", "2002:7f00:1::1", "2002:a00:1::1",
		"3fff::1", "5f00::1", "fc00::1", "fe80::1", "fec0::1", "ff02::1",
	} {
		if !Reserved(netip.MustParseAddr(address)) {
			t.Errorf("%s is set aside for special purposes, and was treated as the internet", address)
		}
	}
}

func TestAClientRefusesTheServersOwnNetwork(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secrets"))
	}))
	defer inside.Close()

	if _, err := Client(2*time.Second, ParseAllow("")).Get(inside.URL); err == nil {
		t.Fatal("a request to the server's own network went through")
	} else if !strings.Contains(err.Error(), ErrBlocked.Error()) {
		t.Fatalf("error = %v, want it to say the address is not reachable", err)
	}

	resp, err := Client(2*time.Second, ParseAllow("127.0.0.0/8, ::1/128")).Get(inside.URL)
	if err != nil {
		t.Fatalf("an allowed address was refused: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// A redirect is dialled like the first address, so an allowed host cannot
// bounce the call into the network.
func TestARedirectIsCheckedAgain(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secrets"))
	}))
	defer inside.Close()
	bouncer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, inside.URL, http.StatusFound)
	}))
	defer bouncer.Close()

	named := strings.Replace(bouncer.URL, "127.0.0.1", "localhost", 1)
	if _, err := Client(2*time.Second, ParseAllow("localhost")).Get(named); err == nil || !strings.Contains(err.Error(), ErrBlocked.Error()) {
		t.Fatalf("a redirect into the network went through: %v", err)
	}
}

func TestAnAllowListReadsHostsAndRanges(t *testing.T) {
	allow := ParseAllow("armature-stub, 10.0.0.0/8, 192.168.1.5, nonsense/99")
	if !allow.hosts["armature-stub"] || !allow.Names("Armature-Stub") {
		t.Error("a host name was not kept")
	}
	if !allow.permits(netip.MustParseAddr("10.9.9.9")) || !allow.Names("192.168.1.5") {
		t.Error("a named range or address was not kept")
	}
	if allow.permits(netip.MustParseAddr("172.16.0.1")) || allow.Names("example.com") {
		t.Error("something nobody named was allowed")
	}
	if allow.hosts["nonsense/99"] {
		t.Error("an unreadable entry was kept as a host name")
	}
}

// Through a proxy the dial reaches the proxy, so the destination is checked
// before anything is sent to it.
func TestAProxiedRequestIsCheckedForItsDestination(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied.Add(1)
		_, _ = w.Write([]byte("forwarded"))
	}))
	defer proxy.Close()
	via, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := WithTransport(2*time.Second, transport(ParseAllow("127.0.0.1/32"), http.ProxyURL(via)))

	for _, target := range []string{"http://10.1.2.3/", "http://169.254.169.254/latest", "http://[64:ff9b::a00:1]/"} {
		_, err := client.Get(target)
		if !errors.Is(err, ErrBlocked) {
			t.Errorf("%s through a proxy: error = %v, want it refused", target, err)
		}
	}
	if n := proxied.Load(); n != 0 {
		t.Fatalf("the proxy was sent %d requests for reserved destinations", n)
	}

	resp, err := client.Get("http://93.184.216.34/")
	if err != nil {
		t.Fatalf("a public destination through the proxy was refused: %v", err)
	}
	defer resp.Body.Close()
	if proxied.Load() != 1 {
		t.Fatal("a public destination did not go through the proxy")
	}
}

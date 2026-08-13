package api

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestValidateWebProxyInput(t *testing.T) {
	valid, host, err := validateWebProxyInput(webProxyInput{HostID: "ssh-host", TargetURL: "https://192.168.1.1:8443/admin"})
	if err != nil || valid.String() != "https://192.168.1.1:8443/admin" || host != "192.168.1.1:8443" {
		t.Fatalf("valid target=%v host=%q err=%v", valid, host, err)
	}
	for name, value := range map[string]webProxyInput{
		"missing ssh host": {TargetURL: "http://192.168.1.1"},
		"unknown scheme":   {HostID: "ssh-host", TargetURL: "ftp://192.168.1.1"},
		"credentials":      {HostID: "ssh-host", TargetURL: "http://admin:secret@192.168.1.1"},
		"fragment":         {HostID: "ssh-host", TargetURL: "http://192.168.1.1/#settings"},
		"bad host header":  {HostID: "ssh-host", TargetURL: "http://192.168.1.1", UpstreamHost: "safe\r\nX-Bad: yes"},
	} {
		if _, _, err := validateWebProxyInput(value); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestRewriteProxyURL(t *testing.T) {
	target, _ := url.Parse("http://192.168.1.1/admin")
	prefix := "/web-proxy/token"
	for input, expected := range map[string]string{
		"/assets/app.js?v=1":                     prefix + "/assets/app.js?v=1",
		"http://192.168.1.1/admin/login#form":    prefix + "/login#form",
		"//192.168.1.1/admin/image.png":          prefix + "/image.png",
		prefix + "/assets/already-proxied.js":    prefix + "/assets/already-proxied.js",
		"relative/page":                          "relative/page",
		"https://cdn.example.invalid/library.js": "https://cdn.example.invalid/library.js",
		"data:image/png;base64,AAAA":             "data:image/png;base64,AAAA",
	} {
		if actual := rewriteProxyURL(input, prefix, target); actual != expected {
			t.Errorf("rewriteProxyURL(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestRewriteHTML(t *testing.T) {
	target, _ := url.Parse("http://router.internal")
	body := rewriteHTML([]byte(`<html><head><script src="/dashboard.js"></script></head><body><a href="/login">Login</a><img src="/logo.png" srcset="/small.png 1x, /large.png 2x"><form action="/save"></form><script>if (1 < 2) console.log("ok")</script></body></html>`), "/web-proxy/token", target)
	text := string(body)
	for _, expected := range []string{
		`data-velin-web-proxy="runtime"`,
		`<base href="/web-proxy/token/" data-velin-web-proxy="base"/>`,
		`const prefix="/web-proxy/token"`,
		`window.fetch=function(input,init)`,
		`XMLHttpRequest.prototype.open=function(method,url)`,
		`href="/web-proxy/token/login"`,
		`src="/web-proxy/token/logo.png"`,
		`srcset="/web-proxy/token/small.png 1x, /web-proxy/token/large.png 2x"`,
		`action="/web-proxy/token/save"`,
		`if (1 < 2) console.log("ok")`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("rewritten HTML missing %q: %s", expected, text)
		}
	}
	if bootstrap, dashboard := strings.Index(text, `data-velin-web-proxy="runtime"`), strings.Index(text, `src="/web-proxy/token/dashboard.js"`); bootstrap < 0 || dashboard < 0 || bootstrap > dashboard {
		t.Fatalf("runtime bootstrap must precede target scripts: %s", text)
	}
}

func TestRewriteStableWebServiceNavigation(t *testing.T) {
	target, _ := url.Parse("http://router.internal")
	prefix := "/web-service-proxy/service-id"
	body := string(rewriteHTML([]byte(`<html><head></head><body><a href="/admin">Admin</a><img src="/logo.png"></body></html>`), prefix, target))
	if !strings.Contains(body, `href="/admin?__velin_web_service=service-id"`) {
		t.Fatalf("navigation link did not use browser route: %s", body)
	}
	if !strings.Contains(body, `src="/web-service-proxy/service-id/logo.png"`) {
		t.Fatalf("resource URL did not use proxy path: %s", body)
	}
	if actual := rewriteBrowserRouteURL("/admin?tab=users", prefix, target); actual != "/admin?__velin_web_service=service-id&amp;tab=users" && actual != "/admin?__velin_web_service=service-id&tab=users" {
		t.Fatalf("browser route=%q", actual)
	}
}

func TestWebProxyBootstrapIncludesTargetMapping(t *testing.T) {
	target, _ := url.Parse("http://192.168.1.1/admin")
	script := webProxyBootstrap("/web-proxy/token", target)
	for _, expected := range []string{
		`const targetHost="192.168.1.1"`,
		`const targetPath="/admin"`,
		`const serviceID=""`,
		`function browserRoute(value)`,
		`history.pushState=function`,
		`navigationAttributes`,
		`parsed.pathname=prefix+`,
		`window.WebSocket=new Proxy`,
		`navigator.sendBeacon=function`,
		`Element.prototype.setAttribute=function`,
		`window.open=function(url)`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("bootstrap missing %q", expected)
		}
	}
}

func TestRewriteCSS(t *testing.T) {
	target, _ := url.Parse("http://router.internal")
	body := rewriteCSS([]byte(`body{background:url('/wall.png')} @import "/theme.css"; .icon{background:url(data:image/png;base64,AAAA)}`), "/web-proxy/token", target)
	text := string(body)
	for _, expected := range []string{
		`url('/web-proxy/token/wall.png')`,
		`@import "/web-proxy/token/theme.css"`,
		`url(data:image/png;base64,AAAA)`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("rewritten CSS missing %q: %s", expected, text)
		}
	}
}

func TestModifyResponseDoesNotRewriteJavaScript(t *testing.T) {
	target, _ := url.Parse("http://router.internal")
	request, _ := http.NewRequest(http.MethodGet, "https://velin.example/web-service-proxy/id/app.js", nil)
	original := `const base="/api/v1";const endpoint=base+"/themes";const route="/admin/*";`
	session := &webProxySession{routePrefix: "/web-service-proxy/id", target: target}
	response := &http.Response{
		Header: http.Header{
			"Content-Type":  {"application/javascript; charset=utf-8"},
			"ETag":          {`"upstream-version"`},
			"Last-Modified": {"Thu, 13 Aug 2026 00:00:00 GMT"},
		},
		Body:          io.NopCloser(strings.NewReader(original)),
		ContentLength: int64(len(original)),
		Request:       request,
	}
	if err := session.modifyResponse(response); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("JavaScript response was modified: %q", body)
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("ETag") != "" || response.Header.Get("Last-Modified") != "" {
		t.Fatalf("unsafe proxy cache headers: %v", response.Header)
	}
}

func TestPathProxyDisablesConditionalUpstreamCache(t *testing.T) {
	target, _ := url.Parse("http://router.internal")
	session := &webProxySession{routePrefix: "/web-service-proxy/id", target: target, upstream: "router.internal"}
	request, _ := http.NewRequest(http.MethodGet, "https://velin.example/web-service-proxy/id/app.js", nil)
	request.Header.Set("If-None-Match", `"cached"`)
	request.Header.Set("If-Modified-Since", "Thu, 13 Aug 2026 00:00:00 GMT")
	session.reverseProxy().Director(request)
	if request.Header.Get("If-None-Match") != "" || request.Header.Get("If-Modified-Since") != "" {
		t.Fatalf("conditional cache headers reached upstream: %v", request.Header)
	}
}

func TestProxyCookieIsolation(t *testing.T) {
	request := &http.Request{Header: http.Header{"Cookie": {"velin_session=secret; csrf=upstream"}}}
	if actual := upstreamCookies(request); actual != "csrf=upstream" {
		t.Fatalf("upstream cookies=%q", actual)
	}
	if strings.Contains(webProxyCSP("velin.example", "/web-proxy/token"), "connect-src 'self'") {
		t.Fatal("proxy CSP allows root-origin API connections")
	}
}

func TestRewriteRequestOrigin(t *testing.T) {
	target, _ := url.Parse("http://router.internal/admin")
	request, _ := http.NewRequest(http.MethodGet, "http://velin.example/web-proxy/token/socket", nil)
	request.Header.Set("Origin", "https://velin.example")
	request.Header.Set("Referer", "https://velin.example/web-proxy/token/device.html?device=abc")

	rewriteRequestOrigin(request, "/web-proxy/token", target, "dashboard.internal:8080")

	if actual := request.Header.Get("Origin"); actual != "http://dashboard.internal:8080" {
		t.Fatalf("origin=%q", actual)
	}
	if actual := request.Header.Get("Referer"); actual != "http://dashboard.internal:8080/admin/device.html?device=abc" {
		t.Fatalf("referer=%q", actual)
	}
}

func TestRequestProto(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "http://velin.example/", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	if actual := requestProto(request); actual != "https" {
		t.Fatalf("proto=%q", actual)
	}
}

func TestJoinRawQuery(t *testing.T) {
	if actual := joinRawQuery("view=full", "page=2"); actual != "view=full&page=2" {
		t.Fatalf("query=%q", actual)
	}
}

func TestLogRequestPathRedactsProxyToken(t *testing.T) {
	if actual := logRequestPath("/web-proxy/secret-token/assets/app.js"); actual != "/web-proxy/[redacted]/assets/app.js" {
		t.Fatalf("redacted path=%q", actual)
	}
	if actual := logRequestPath("/api/hosts"); actual != "/api/hosts" {
		t.Fatalf("regular path=%q", actual)
	}
	if actual := logRequestPath("/web-service-proxy/service-id/assets/app.js"); actual != "/web-service-proxy/[redacted]/assets/app.js" {
		t.Fatalf("stable proxy path=%q", actual)
	}
}

func TestStableWebProxyPrefix(t *testing.T) {
	session := &webProxySession{token: "temporary", routePrefix: stableWebProxyPrefix("service-id")}
	if actual := session.prefix(); actual != "/web-service-proxy/service-id" {
		t.Fatalf("stable prefix=%q", actual)
	}
	session.routePrefix = ""
	if actual := session.prefix(); actual != "/web-proxy/temporary" {
		t.Fatalf("temporary prefix=%q", actual)
	}
	session.rootProxy = true
	if actual := session.prefix(); actual != "" {
		t.Fatalf("root proxy prefix=%q", actual)
	}
}

func TestRootProxyKeepsRequestAtRoot(t *testing.T) {
	target, _ := url.Parse("http://router.internal/admin")
	session := &webProxySession{rootProxy: true, target: target, upstream: "router.internal"}
	request, _ := http.NewRequest(http.MethodGet, "http://velin.example:18080/assets/app.js?x=1", nil)
	session.reverseProxy().Director(request)
	if request.URL.String() != "http://router.internal/admin/assets/app.js?x=1" {
		t.Fatalf("root proxy URL=%q", request.URL.String())
	}
	if request.Header.Get("X-Forwarded-Prefix") != "" {
		t.Fatalf("root proxy forwarded prefix=%q", request.Header.Get("X-Forwarded-Prefix"))
	}
}

func TestRootProxyCannotOverwriteVelinSession(t *testing.T) {
	target, _ := url.Parse("http://router.internal")
	session := &webProxySession{rootProxy: true, target: target}
	request, _ := http.NewRequest(http.MethodGet, "http://velin.example:18080/", nil)
	response := &http.Response{
		Header:  http.Header{"Set-Cookie": {cookieName + "=attacker; Path=/", "upstream=value; Path=/"}},
		Body:    io.NopCloser(bytes.NewReader(nil)),
		Request: request,
	}
	if err := session.modifyResponse(response); err != nil {
		t.Fatal(err)
	}
	cookies := response.Header.Values("Set-Cookie")
	if len(cookies) != 1 || strings.Contains(cookies[0], cookieName+"=") || !strings.Contains(cookies[0], "upstream=value") {
		t.Fatalf("root proxy cookies=%v", cookies)
	}
}

func TestHostPortValidation(t *testing.T) {
	manager := &webProxyManager{hostPorts: make(map[string]*hostPortWebProxy)}
	if err := manager.checkHostPort("service", 0); err == nil {
		t.Fatal("invalid host port was accepted")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err = manager.checkHostPort("service", port); err == nil {
		t.Fatal("occupied host port was accepted")
	}
	manager.hostPorts["other"] = &hostPortWebProxy{port: port + 1}
	if err = manager.checkHostPort("service", port+1); err == nil {
		t.Fatal("duplicate configured host port was accepted")
	}
}

func TestHostPortListenerLifecycle(t *testing.T) {
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	manager := &webProxyManager{hostPorts: make(map[string]*hostPortWebProxy)}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	if err = manager.setHostPort("service", port, handler); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("host port response=%d %q", response.StatusCode, body)
	}
	manager.deleteHostPort("service")
	released, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("host port was not released: %v", err)
	}
	_ = released.Close()
}

func TestHostPortURL(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://[2001:db8::1]:8377/api/web-services/id/open", nil)
	if actual := hostPortWebProxyURL(request, 18080); actual != "http://[2001:db8::1]:18080/" {
		t.Fatalf("host port URL=%q", actual)
	}
	request.Host = "velin.example"
	if actual := hostPortWebProxyURL(request, 18080); actual != "http://velin.example:18080/" {
		t.Fatalf("domain host port URL=%q", actual)
	}
	if actual := configuredListenPort("0.0.0.0:8377"); actual != 8377 {
		t.Fatalf("configured listen port=%d", actual)
	}
}

func TestRootProxyCSPUsesRootPath(t *testing.T) {
	policy := webProxyCSP("velin.example:18080", "")
	if strings.Contains(policy, "velin.example:18080//") || !strings.Contains(policy, "ws://velin.example:18080/") {
		t.Fatalf("root proxy CSP=%q", policy)
	}
}

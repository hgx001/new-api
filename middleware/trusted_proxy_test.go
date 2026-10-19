package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestConfigureTrustedProxiesRejectsForgedForwardedIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := ConfigureTrustedProxies(engine, []string{"172.18.0.1"}); err != nil {
		t.Fatalf("ConfigureTrustedProxies() error = %v", err)
	}
	engine.GET("/client-ip", func(c *gin.Context) {
		c.String(200, c.ClientIP())
	})

	request := httptest.NewRequest("GET", "/client-ip", nil)
	request.RemoteAddr = "172.18.0.1:43210"                            // nginx peer in production Docker bridge
	request.Header.Set("X-Forwarded-For", "172.18.0.1, 198.51.100.23") // untrusted/appendable chain
	request.Header.Set("X-Real-IP", "198.51.100.23")                   // nginx overwrites this with $remote_addr
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if got, want := response.Body.String(), "198.51.100.23"; got != want {
		t.Fatalf("ClientIP() = %q, want X-Real-IP from trusted proxy %q", got, want)
	}
}

func TestConfigureTrustedProxiesIgnoresHeadersFromUntrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := ConfigureTrustedProxies(engine, []string{"172.18.0.1"}); err != nil {
		t.Fatalf("ConfigureTrustedProxies() error = %v", err)
	}
	engine.GET("/client-ip", func(c *gin.Context) {
		c.String(200, c.ClientIP())
	})

	request := httptest.NewRequest("GET", "/client-ip", nil)
	request.RemoteAddr = "203.0.113.9:54321"
	request.Header.Set("X-Forwarded-For", "172.18.0.1")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if got, want := response.Body.String(), "203.0.113.9"; got != want {
		t.Fatalf("ClientIP() = %q, want untrusted peer address %q", got, want)
	}
}

func TestConfigureTrustedProxiesRejectsInvalidConfiguration(t *testing.T) {
	engine := gin.New()
	if err := ConfigureTrustedProxies(engine, []string{"not-an-ip-or-cidr"}); err == nil {
		t.Fatal("ConfigureTrustedProxies() error = nil, want invalid proxy configuration error")
	}
}

func TestParseTrustedProxyAddressesDefaultsToProductionGateway(t *testing.T) {
	got := ParseTrustedProxyAddresses("")
	if len(got) != 1 || got[0] != DefaultTrustedProxyAddresses {
		t.Fatalf("ParseTrustedProxyAddresses(\"\") = %#v, want [%q]", got, DefaultTrustedProxyAddresses)
	}
}

func TestParseTrustedProxyAddressesSupportsExplicitProxyList(t *testing.T) {
	got := ParseTrustedProxyAddresses(" 10.0.0.1 , 192.0.2.0/24 ")
	if len(got) != 2 || got[0] != "10.0.0.1" || got[1] != "192.0.2.0/24" {
		t.Fatalf("ParseTrustedProxyAddresses() = %#v, want trimmed IP/CIDR list", got)
	}
}

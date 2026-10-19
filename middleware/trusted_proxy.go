package middleware

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// DefaultTrustedProxyAddresses matches the Docker bridge gateway used by the
// production backend deployment. Override it only when the reverse-proxy peer
// address is different, using TRUSTED_PROXIES as a comma-separated IP/CIDR list.
const DefaultTrustedProxyAddresses = "172.18.0.1"

// ParseTrustedProxyAddresses returns explicitly configured trusted proxies, or
// the production Docker gateway when no override is provided.
func ParseTrustedProxyAddresses(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{DefaultTrustedProxyAddresses}
	}
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// ConfigureTrustedProxies limits forwarded client-IP headers to explicitly trusted
// reverse proxies. An empty list disables forwarded-header processing entirely.
func ConfigureTrustedProxies(engine *gin.Engine, proxies []string) error {
	if engine == nil {
		return fmt.Errorf("gin engine must not be nil")
	}
	for i := range proxies {
		proxies[i] = strings.TrimSpace(proxies[i])
		if proxies[i] == "" {
			return fmt.Errorf("trusted proxy entry %d is empty", i)
		}
	}
	return engine.SetTrustedProxies(proxies)
}

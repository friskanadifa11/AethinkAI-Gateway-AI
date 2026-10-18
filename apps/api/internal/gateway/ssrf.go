package gateway

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Cloud metadata endpoints that turn an SSRF into credential theft. These are
// refused unconditionally: no legitimate self-hosted provider lives there.
var blockedHosts = map[string]bool{
	"169.254.169.254":          true, // AWS/GCP/Azure IMDS
	"metadata.google.internal": true,
	"100.100.100.200":          true, // Alibaba Cloud
	"fd00:ec2::254":            true,
	"metadata":                 true,
}

// ValidateProviderURL applies the base-URL checks the PRD requires before a
// provider is saved or connection-tested.
//
// allowPrivate keeps the gateway usable on the private network it is deployed
// on (a self-hosted gateway usually talks to an upstream on the LAN) while
// still refusing the metadata range that makes SSRF dangerous.
func ValidateProviderURL(raw string, allowPrivate bool) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("base URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("base URL is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("base URL must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("base URL has no host")
	}
	if parsed.User != nil {
		// Credentials in a URL end up in logs and Referer headers.
		return fmt.Errorf("base URL must not embed credentials")
	}
	host := strings.ToLower(parsed.Hostname())
	if blockedHosts[host] {
		return fmt.Errorf("base URL points at a cloud metadata endpoint")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("base URL must not point at a link-local address")
		}
		if ip.IsLoopback() && !allowPrivate {
			return fmt.Errorf("loopback base URLs are disabled on this installation")
		}
		if isPrivate(ip) && !allowPrivate {
			return fmt.Errorf("private base URLs are disabled on this installation")
		}
	}
	return nil
}

func isPrivate(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	return false
}

// redactHost renders a provider host for display without its port-less path,
// keeping credentials out of the console entirely.
func redactHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

package common

import (
	"errors"
	"fmt"
	"strings"
)

var ErrEmailDomainBlocked = errors.New("this email domain is not allowed for registration or binding")

// ValidateEmailDomainBlacklist accepts comma-separated DNS domains, not email
// addresses, URLs or wildcard patterns. International domains use punycode.
func ValidateEmailDomainBlacklist(value string) error {
	for _, entry := range strings.Split(value, ",") {
		domain := strings.ToLower(strings.TrimSpace(entry))
		if domain == "" {
			continue
		}
		if len(domain) > 253 || !strings.Contains(domain, ".") || Validate.Var(domain, "hostname_rfc1123") != nil {
			return fmt.Errorf("invalid email blacklist domain %q: enter a domain such as maildrop.cc", entry)
		}
	}
	return nil
}

// IsEmailDomainBlocked checks the independently enabled blacklist. A domain
// blocks itself and its subdomains, but never another domain with a similar name.
func IsEmailDomainBlocked(email string) bool {
	_, domain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if !ok || domain == "" {
		return false
	}
	// DNS root dots do not change the domain identity. Normalize both sides
	// so an absolute domain cannot bypass a blacklist entry (or vice versa).
	domain = strings.TrimRight(domain, ".")
	OptionMapRWMutex.RLock()
	enabled := OptionMap["EmailDomainBlacklistEnabled"] == "true"
	blacklist := OptionMap["EmailDomainBlacklist"]
	OptionMapRWMutex.RUnlock()
	if !enabled {
		return false
	}
	for _, entry := range strings.Split(blacklist, ",") {
		blocked := strings.TrimRight(strings.ToLower(strings.TrimSpace(entry)), ".")
		if blocked != "" && (domain == blocked || strings.HasSuffix(domain, "."+blocked)) {
			return true
		}
	}
	return false
}

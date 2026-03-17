package hostnameutils

import (
	"fmt"
	"net"
	"strings"
)

// IsHostNameInValidFormat follows RFC1123 requirement except
// 1. no IP allowed
// 2. wildcard is only allowed as leftmost character
// Allowed Characters: Hostname labels must only contain lowercase ASCII letters (a-z), digits (0-9), and hyphens (-).
// Starting with a Digit: RFC 1123 allows labels to begin with a digit, which is a departure from the previous RFC 952 restriction.
// Length: Each label in a hostname can be between 1 and 63 characters long.
// Overall Hostname Length: The entire hostname, including the periods separating labels, cannot exceed 253 characters.
// Case: Hostnames are case-insensitive.
// Underscore: Underscores are not permitted in hostnames.
// Other Symbols: No other symbols, punctuation, or whitespace is allowed in hostnames
// Most of the requirements above is already checked by CRD pattern: Pattern=`^(\*\.)?[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
// Thus this function only checks for 1. if it is IP 2. label length is between 1 and 63
func IsHostNameInValidFormat(hostName string) (bool, error) {
	if net.ParseIP(hostName) != nil {

		return false, fmt.Errorf("hostname can not be IP address")
	}
	labels := strings.Split(hostName, ".")
	if strings.HasPrefix(hostName, "*.") {
		labels = labels[1:]
	}
	for _, label := range labels {
		if len(label) < 1 || len(label) > 63 {
			return false, fmt.Errorf("invalid hostname label length, length must between 1 and 63")
		}
	}
	return true, nil
}

// IsHostnameCompatible checks if given two hostnames are compatible with each other
// this function is used to check if listener hostname and Route hostname match
func IsHostnameCompatible(hostnameOne, hostnameTwo string) bool {
	_, isCompatible := GetCompatibleHostname(hostnameOne, hostnameTwo)
	return isCompatible
}

// GetCompatibleHostname returns the more specific hostname if two hostnames are compatible.
// Two hostnames are compatible if:
//  1. They are exactly the same (e.g., "example.com" and "example.com")
//  2. One is a wildcard that matches the other (e.g., "*.example.com" matches "api.example.com")
//
// When compatible, returns the more specific hostname (non-wildcard) and true.
// When incompatible (e.g., "api.example.com" vs "web.example.com"), returns empty string and false.
// This is used to match Gateway listener hostnames with Route hostnames for traffic routing.
func GetCompatibleHostname(hostnameOne, hostnameTwo string) (string, bool) {
	// exact match
	if hostnameOne == hostnameTwo {
		return hostnameOne, true
	}

	// hostnameOne is wildcard, hostnameTwo matches - return hostnameTwo (more specific)
	if strings.HasPrefix(hostnameOne, "*.") && strings.HasSuffix(hostnameTwo, hostnameOne[1:]) {
		return hostnameTwo, true
	}

	// hostnameTwo is wildcard, hostnameOne matches - return hostnameOne (more specific)
	if strings.HasPrefix(hostnameTwo, "*.") && strings.HasSuffix(hostnameOne, hostnameTwo[1:]) {
		return hostnameOne, true
	}

	return "", false
}

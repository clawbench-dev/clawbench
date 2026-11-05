package mail

import (
	"strings"

	"golang.org/x/net/idna"
)

// normalizeAddresses rewrites internationalized domain names into their ASCII
// (punycode) form.
//
// `bob@例子.com` is an ordinary address, but the SMTP envelope has no way to
// carry the domain: it is a DNS name, and DNS only understands punycode. Doing
// the conversion before the message is built keeps the envelope and the headers
// saying the same thing, and leaves only a non-ASCII *local* part as a genuine
// SMTPUTF8 requirement.
func normalizeAddresses(req SendRequest) SendRequest {
	req.From = normalizeAddress(req.From)
	req.To = normalizeAddressList(req.To)
	req.Cc = normalizeAddressList(req.Cc)
	req.Bcc = normalizeAddressList(req.Bcc)
	return req
}

func normalizeAddressList(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, len(in))
	for i, addr := range in {
		out[i] = normalizeAddress(addr)
	}
	return out
}

func normalizeAddress(addr string) string {
	at := strings.LastIndexByte(addr, '@')
	if at < 0 || at == len(addr)-1 {
		return addr
	}
	local, domain := addr[:at+1], addr[at+1:]

	// A display-name form ("Name <bob@例子.com>") leaves the closing bracket
	// attached to the domain; keep it where it was.
	suffix := ""
	if trimmed := strings.TrimSuffix(domain, ">"); trimmed != domain {
		domain, suffix = trimmed, ">"
	}
	if isASCII(domain) {
		return addr
	}
	ascii, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		// Leave it alone rather than guess: the send then fails with the clear
		// "no SMTPUTF8" message (or goes through on a server that has it)
		// instead of us rewriting the address into somebody else's mailbox.
		return addr
	}
	return local + ascii + suffix
}

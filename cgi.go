// Copyright (c) the go-ruby-cgi/cgi authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package cgi is a pure-Go (no cgo) reimplementation of the deterministic
// escaping and query-parsing surface of Ruby's CGI utility methods, byte-for-byte
// compatible with MRI 4.0.5 (the cgi/escape default + the cgi gem's CGI.parse).
//
// It is the CGI backend for go-embedded-ruby, but is a standalone, reusable
// module with no dependency on the Ruby runtime — a sibling of go-ruby-yaml,
// go-ruby-regexp and go-ruby-erb.
//
// Only the pure-compute surface lives here: URL/form escaping (Escape /
// Unescape), the URI-component variants (EscapeURIComponent /
// UnescapeURIComponent), the HTML-entity helpers (EscapeHTML / UnescapeHTML,
// with numeric &#NN; / &#xHH; decoding), the element helpers (EscapeElement /
// UnescapeElement), and query parsing (ParseQuery). The request/response/session
// machinery that needs a live server is out of scope.
package cgi

import (
	"strings"
)

// unreserved reports whether b is in CGI's unreserved set: ASCII letters,
// digits, and the four marks '-', '.', '_', '~'. Both CGI.escape and
// CGI.escapeURIComponent keep exactly these bytes literal (they differ only in
// how they encode the space).
func unreserved(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z',
		b >= 'a' && b <= 'z',
		b >= '0' && b <= '9':
		return true
	case b == '-', b == '.', b == '_', b == '~':
		return true
	}
	return false
}

const upperhex = "0123456789ABCDEF"

// pctEncode appends "%XX" (uppercase hex) for byte b to sb.
func pctEncode(sb *strings.Builder, b byte) {
	sb.WriteByte('%')
	sb.WriteByte(upperhex[b>>4])
	sb.WriteByte(upperhex[b&0x0f])
}

// Escape implements Ruby's CGI.escape: application/x-www-form-urlencoded
// encoding. Every byte outside the unreserved set is percent-encoded, and a
// space becomes '+'.
//
//	CGI.escape("a b&c") # => "a+b%26c"
func Escape(s string) string {
	// Fast path: nothing to change.
	if !needsEscape(s, true) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case unreserved(b):
			sb.WriteByte(b)
		case b == ' ':
			sb.WriteByte('+')
		default:
			pctEncode(&sb, b)
		}
	}
	return sb.String()
}

// EscapeURIComponent implements Ruby's CGI.escapeURIComponent (added in the
// 3.5/4.0 era). It is like Escape but encodes a space as "%20" rather than '+'.
//
//	CGI.escapeURIComponent("a b") # => "a%20b"
func EscapeURIComponent(s string) string {
	if !needsEscape(s, false) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		b := s[i]
		if unreserved(b) {
			sb.WriteByte(b)
		} else {
			pctEncode(&sb, b)
		}
	}
	return sb.String()
}

// needsEscape reports whether s contains any byte that Escape /
// EscapeURIComponent would alter. plusForSpace selects the Escape semantics
// (where ' ' is altered to '+'); when false ' ' is still altered (to "%20"),
// so a bare space always counts as needing escape — the parameter is kept for
// symmetry but a space is non-unreserved in both modes.
func needsEscape(s string, plusForSpace bool) bool {
	_ = plusForSpace
	for i := 0; i < len(s); i++ {
		if !unreserved(s[i]) {
			return true
		}
	}
	return false
}

// hexVal returns the value of an ASCII hex digit and whether it was valid.
func hexVal(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// Unescape implements Ruby's CGI.unescape: it decodes "%XX" escapes and turns
// '+' into a space. Malformed escapes (a '%' not followed by two hex digits)
// are left exactly as-is — CGI.unescape never raises.
//
//	CGI.unescape("a+b%26c") # => "a b&c"
//	CGI.unescape("%zz%2")   # => "%zz%2"
func Unescape(s string) string {
	if !strings.ContainsAny(s, "%+") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '+':
			sb.WriteByte(' ')
		case '%':
			if i+2 < len(s) {
				hi, ok1 := hexVal(s[i+1])
				lo, ok2 := hexVal(s[i+2])
				if ok1 && ok2 {
					sb.WriteByte(hi<<4 | lo)
					i += 2
					continue
				}
			}
			sb.WriteByte('%')
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// UnescapeURIComponent implements Ruby's CGI.unescapeURIComponent. It is like
// Unescape but does NOT turn '+' into a space (only "%XX" is decoded).
//
//	CGI.unescapeURIComponent("a%20b+c") # => "a b+c"
func UnescapeURIComponent(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := hexVal(s[i+1])
			lo, ok2 := hexVal(s[i+2])
			if ok1 && ok2 {
				sb.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

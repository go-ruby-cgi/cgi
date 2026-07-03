// Copyright (c) the go-ruby-cgi/cgi authors
//
// SPDX-License-Identifier: BSD-3-Clause

package cgi

import (
	"strings"
	"unsafe"
)

// htmlEscapes is a 256-entry lookup from a byte to the HTML entity CGI.escapeHTML
// emits for it, or the empty string when the byte is copied verbatim. Only the
// five characters MRI's cgi/escape C extension encodes have entries; the
// apostrophe is the numeric "&#39;" (matching MRI), not "&apos;". Indexing this
// array by a byte value is bounds-check-free, so the escape scan is a single
// table load per byte with no branch tree.
var htmlEscapes = [256]string{
	'&':  "&amp;",
	'<':  "&lt;",
	'>':  "&gt;",
	'"':  "&quot;",
	'\'': "&#39;",
}

// EscapeHTML implements Ruby's CGI.escapeHTML: it replaces '&', '<', '>', '"'
// and a single quote with their HTML entities (the single quote becoming
// "&#39;").
//
// It mirrors the table-driven native path of MRI's cgi/escape C extension: a
// single pass over the bytes copies each verbatim run in bulk and splices in the
// entity for each escapable byte, straight into one output buffer. Inputs with
// nothing to escape return the original string with no allocation at all.
//
//	CGI.escapeHTML(`<a href="x">&'`) # => `&lt;a href=&quot;x&quot;&gt;&amp;&#39;`
func EscapeHTML(s string) string {
	// Fast path: find the first byte that needs escaping. If there is none the
	// input is returned untouched, with no allocation.
	i := 0
	for i < len(s) && len(htmlEscapes[s[i]]) == 0 {
		i++
	}
	if i == len(s) {
		return s
	}
	// One output allocation. 2×len comfortably covers any realistic HTML — the
	// escapable bytes are a small fraction of real markup — and append grows the
	// buffer safely in the rare high-density case, so correctness never depends
	// on the estimate.
	buf := make([]byte, 0, 2*len(s))
	buf = append(buf, s[:i]...)
	last := i
	for ; i < len(s); i++ {
		e := htmlEscapes[s[i]]
		if len(e) == 0 {
			continue
		}
		buf = append(buf, s[last:i]...) // bulk-copy the safe run before this byte
		buf = append(buf, e...)         // splice in the entity
		last = i + 1
	}
	buf = append(buf, s[last:]...) // trailing safe run
	// buf is freshly allocated and never aliases s, so the zero-copy conversion
	// is safe (this is exactly what strings.Builder does internally).
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

// maxNumericCodepoint is the largest code point CGI.unescapeHTML will decode. A
// numeric entity for 0x10FFFF or above is left verbatim (MRI's decoder rejects
// it), so the accepted range is 0..0x10FFFE inclusive.
const maxNumericCodepoint = 0x10FFFF - 1

// UnescapeHTML implements Ruby's CGI.unescapeHTML. It decodes the five named
// entities (amp, lt, gt, quot, apos), decimal numeric entities ("&#NN;") and
// hexadecimal numeric entities ("&#xHH;" / "&#XHH;"). A numeric code point is
// emitted as its raw UTF-8 byte sequence using Ruby's permissive encoder (so
// surrogate code points yield their three-byte form, as MRI does). Anything not
// recognised — an unknown name, an empty or overflowing number, a '&' without a
// terminating ';' — is left exactly as it appears.
//
// Like MRI, the numeric decoding assumes a Unicode (UTF-8) receiver: it always
// produces the UTF-8 bytes of the code point.
//
// The scan mirrors MRI's C decoder: it jumps '&' to '&' with IndexByte, copies
// each verbatim run in bulk, and decodes entities straight into the output
// buffer with no per-entity allocation (named entities resolve to a constant
// string, numeric ones write their UTF-8 bytes in place).
//
//	CGI.unescapeHTML("&amp;&#65;&#x42;") # => "&AB"
func UnescapeHTML(s string) string {
	i := strings.IndexByte(s, '&')
	if i < 0 {
		// No '&' at all: nothing can decode, return the input untouched.
		return s
	}
	// One allocation: the decoded output is never longer than the input.
	buf := make([]byte, 0, len(s))
	buf = append(buf, s[:i]...)
	for i < len(s) {
		// s[i] == '&': find the terminating ';'.
		rest := s[i+1:]
		semi := strings.IndexByte(rest, ';')
		if semi < 0 {
			// No ';' anywhere after this '&': nothing more can decode.
			buf = append(buf, s[i:]...)
			break
		}
		if b, ok := decodeEntity(buf, rest[:semi]); ok {
			buf = b
			i += 1 + semi + 1 // past '&', body and ';'
		} else {
			// Not a recognised entity: emit the '&' literally and resume
			// scanning after it, so a later '&' in the body still decodes.
			buf = append(buf, '&')
			i++
		}
		// Bulk-copy the verbatim run up to the next '&'.
		next := strings.IndexByte(s[i:], '&')
		if next < 0 {
			buf = append(buf, s[i:]...)
			break
		}
		buf = append(buf, s[i:i+next]...)
		i += next
	}
	// buf is freshly allocated and never aliases s, so the zero-copy conversion
	// is safe. buf always holds at least the run copied before the first '&' plus
	// one further byte, so it is never empty here.
	return unsafe.String(unsafe.SliceData(buf), len(buf))
}

// decodeEntity decodes the body of an entity (the text strictly between '&' and
// ';') into buf. It returns the extended buffer and whether body was a
// recognised entity; on a miss buf is returned unchanged. Named entities resolve
// to a constant one-byte string (no allocation); numeric entities write their
// UTF-8 bytes directly into buf.
func decodeEntity(buf []byte, body string) ([]byte, bool) {
	if body == "" {
		return buf, false
	}
	if body[0] == '#' {
		return decodeNumeric(buf, body[1:])
	}
	switch body {
	case "amp":
		return append(buf, '&'), true
	case "lt":
		return append(buf, '<'), true
	case "gt":
		return append(buf, '>'), true
	case "quot":
		return append(buf, '"'), true
	case "apos":
		return append(buf, '\''), true
	}
	return buf, false
}

// decodeNumeric decodes the digits of a numeric entity (the text after "&#")
// into buf. "xHH"/"XHH" is hexadecimal, otherwise decimal. An empty,
// non-numeric, or out-of-range value is rejected with buf returned unchanged.
func decodeNumeric(buf []byte, digits string) ([]byte, bool) {
	if digits == "" {
		return buf, false
	}
	var cp int
	if digits[0] == 'x' || digits[0] == 'X' {
		hexDigits := digits[1:]
		if hexDigits == "" {
			return buf, false
		}
		for i := 0; i < len(hexDigits); i++ {
			v, ok := hexVal(hexDigits[i])
			if !ok {
				return buf, false
			}
			cp = cp<<4 | int(v)
			if cp > maxNumericCodepoint {
				return buf, false
			}
		}
	} else {
		for i := 0; i < len(digits); i++ {
			b := digits[i]
			if b < '0' || b > '9' {
				return buf, false
			}
			cp = cp*10 + int(b-'0')
			if cp > maxNumericCodepoint {
				return buf, false
			}
		}
	}
	return appendCodepoint(buf, cp), true
}

// appendCodepoint appends the raw UTF-8 byte sequence for the code point cp to
// buf, matching Ruby's [cp].pack("U") encoder. Unlike Go's utf8.EncodeRune it
// does not substitute U+FFFD for surrogates (0xD800..0xDFFF): it emits their
// literal three-byte form, exactly as MRI's CGI.unescapeHTML does. cp is
// guaranteed to be in 0..0x10FFFE by the caller.
func appendCodepoint(buf []byte, cp int) []byte {
	switch {
	case cp < 0x80:
		return append(buf, byte(cp))
	case cp < 0x800:
		return append(buf,
			byte(0xC0|cp>>6),
			byte(0x80|cp&0x3F),
		)
	case cp < 0x10000:
		return append(buf,
			byte(0xE0|cp>>12),
			byte(0x80|(cp>>6)&0x3F),
			byte(0x80|cp&0x3F),
		)
	default:
		return append(buf,
			byte(0xF0|cp>>18),
			byte(0x80|(cp>>12)&0x3F),
			byte(0x80|(cp>>6)&0x3F),
			byte(0x80|cp&0x3F),
		)
	}
}

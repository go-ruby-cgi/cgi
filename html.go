// Copyright (c) the go-ruby-cgi/cgi authors
//
// SPDX-License-Identifier: BSD-3-Clause

package cgi

import (
	"strings"
)

// htmlEscaper maps the five characters CGI.escapeHTML encodes to their HTML
// entities. The apostrophe is encoded as the numeric "&#39;" — matching MRI's
// cgi/escape — not "&apos;".
var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#39;",
)

// EscapeHTML implements Ruby's CGI.escapeHTML: it replaces '&', '<', '>', '"'
// and a single quote with their HTML entities (the single quote becoming
// "&#39;").
//
//	CGI.escapeHTML(`<a href="x">&'`) # => `&lt;a href=&quot;x&quot;&gt;&amp;&#39;`
func EscapeHTML(s string) string {
	return htmlEscaper.Replace(s)
}

// namedEntities is the exact set of named entities CGI.unescapeHTML decodes.
// MRI's decoder recognises only these five names (case-sensitively); every
// other "&name;" is left verbatim. Note that "&apos;" decodes even though
// EscapeHTML emits "&#39;".
var namedEntities = map[string]byte{
	"amp":  '&',
	"lt":   '<',
	"gt":   '>',
	"quot": '"',
	"apos": '\'',
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
//	CGI.unescapeHTML("&amp;&#65;&#x42;") # => "&AB"
func UnescapeHTML(s string) string {
	i := strings.IndexByte(s, '&')
	if i < 0 {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	sb.WriteString(s[:i])
	for i < len(s) {
		if s[i] != '&' {
			sb.WriteByte(s[i])
			i++
			continue
		}
		// s[i] == '&': find the terminating ';'.
		semi := strings.IndexByte(s[i:], ';')
		if semi < 0 {
			// No ';' anywhere after this '&': nothing more can decode.
			sb.WriteString(s[i:])
			break
		}
		body := s[i+1 : i+semi] // text between '&' and ';'
		if dec, ok := decodeEntity(body); ok {
			sb.WriteString(dec)
			i += semi + 1
			continue
		}
		// Not a recognised entity: emit the '&' literally and resume scanning
		// after it, so a later '&' in the body still gets a chance.
		sb.WriteByte('&')
		i++
	}
	return sb.String()
}

// decodeEntity decodes the body of an entity (the text strictly between '&' and
// ';'). It returns the decoded string and whether body was a recognised entity.
func decodeEntity(body string) (string, bool) {
	if body == "" {
		return "", false
	}
	if body[0] == '#' {
		return decodeNumeric(body[1:])
	}
	if b, ok := namedEntities[body]; ok {
		return string(rune(b)), true
	}
	return "", false
}

// decodeNumeric decodes the digits of a numeric entity (the text after "&#").
// "xHH"/"XHH" is hexadecimal, otherwise decimal. An empty, non-numeric, or
// out-of-range value is rejected.
func decodeNumeric(digits string) (string, bool) {
	if digits == "" {
		return "", false
	}
	var cp int
	if digits[0] == 'x' || digits[0] == 'X' {
		hexDigits := digits[1:]
		if hexDigits == "" {
			return "", false
		}
		for i := 0; i < len(hexDigits); i++ {
			v, ok := hexVal(hexDigits[i])
			if !ok {
				return "", false
			}
			cp = cp<<4 | int(v)
			if cp > maxNumericCodepoint {
				return "", false
			}
		}
	} else {
		for i := 0; i < len(digits); i++ {
			b := digits[i]
			if b < '0' || b > '9' {
				return "", false
			}
			cp = cp*10 + int(b-'0')
			if cp > maxNumericCodepoint {
				return "", false
			}
		}
	}
	return encodeCodepoint(cp), true
}

// encodeCodepoint returns the raw UTF-8 byte sequence for the code point cp,
// matching Ruby's [cp].pack("U") encoder. Unlike Go's utf8.EncodeRune it does
// not substitute U+FFFD for surrogates (0xD800..0xDFFF): it emits their literal
// three-byte form, exactly as MRI's CGI.unescapeHTML does. cp is guaranteed to
// be in 0..0x10FFFE by the caller.
func encodeCodepoint(cp int) string {
	switch {
	case cp < 0x80:
		return string([]byte{byte(cp)})
	case cp < 0x800:
		return string([]byte{
			byte(0xC0 | cp>>6),
			byte(0x80 | cp&0x3F),
		})
	case cp < 0x10000:
		return string([]byte{
			byte(0xE0 | cp>>12),
			byte(0x80 | (cp>>6)&0x3F),
			byte(0x80 | cp&0x3F),
		})
	default:
		return string([]byte{
			byte(0xF0 | cp>>18),
			byte(0x80 | (cp>>12)&0x3F),
			byte(0x80 | (cp>>6)&0x3F),
			byte(0x80 | cp&0x3F),
		})
	}
}

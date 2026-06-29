// Copyright (c) the go-ruby-cgi/cgi authors
//
// SPDX-License-Identifier: BSD-3-Clause

package cgi

import (
	"regexp"
	"strings"
)

// EscapeElement implements Ruby's CGI.escapeElement(string, *elements). It
// HTML-escapes (per EscapeHTML) only the start and end tags of the named
// elements, leaving the rest of the string untouched. Element names are matched
// case-insensitively at a word boundary, so EscapeElement("<A><AB>", "A") only
// touches the "<A>". When no elements are given the string is returned verbatim.
//
//	EscapeElement(`<BR><A HREF="url"></A>`, "A")
//	  // => `<BR>&lt;A HREF=&quot;url&quot;&gt;&lt;/A&gt;`
func EscapeElement(s string, elements ...string) string {
	if len(elements) == 0 {
		return s
	}
	re := escapeElementRe(elements)
	return re.ReplaceAllStringFunc(s, EscapeHTML)
}

// escapeElementRe builds the regexp matching a start or end tag of any named
// element, equivalent to Ruby's /<\/?(?:E1|E2|…)\b[^<>]*+>?/im. Go's RE2 has no
// possessive quantifier, but a greedy [^<>]* over a class that excludes '>'
// matches the same text here, so the plain quantifier is faithful.
func escapeElementRe(elements []string) *regexp.Regexp {
	alt := joinElements(elements)
	return regexp.MustCompile(`(?i)<\/?(?:` + alt + `)\b[^<>]*>?`)
}

// joinElements escapes each element name for use in a regexp and joins them with
// '|'. Element names are arbitrary Ruby strings, so they are quoted to avoid
// metacharacter surprises.
func joinElements(elements []string) string {
	quoted := make([]string, len(elements))
	for i, e := range elements {
		quoted[i] = regexp.QuoteMeta(e)
	}
	return strings.Join(quoted, "|")
}

// tagHeadRe matches the escaped tag head "&lt;" optionally followed by '/' and
// then a named element at a word boundary — equivalent to the Ruby prefix
// /&lt;\/?(?:E1|E2|…)\b/im. The whole pattern carries Ruby's /i flag, so the
// "&lt;" entity literal and the element name both match case-insensitively (a
// span like "&LT;a&gt;" matches). The match span is later fed through UnescapeHTML,
// whose entity table IS case-sensitive, so an upper-cased entity inside the span
// (e.g. "&GT;") is included but left un-decoded — exactly mirroring MRI, where
// the gsub captures the span and unescapeHTML transforms it.
func tagHeadRe(elements []string) *regexp.Regexp {
	alt := joinElements(elements)
	return regexp.MustCompile(`(?i)^&lt;/?(?:` + alt + `)\b`)
}

// UnescapeElement implements Ruby's CGI.unescapeElement(string, *elements). It
// reverses EscapeElement: it HTML-unescapes (per UnescapeHTML) only the escaped
// start and end tags of the named elements, leaving the rest untouched. When no
// elements are given the string is returned verbatim.
//
//	UnescapeElement(EscapeHTML(`<BR><A HREF="url"></A>`), "A")
//	  // => `&lt;BR&gt;<A HREF="url"></A>`
//
// It faithfully reproduces Ruby's regexp
//
//	/&lt;\/?(?:E…)\b(?>[^&]+|&(?![gl]t;)\w+;)*(?:&gt;)?/im
//
// by scanning for a tag head, then consuming the tag body (runs of non-'&' text
// or entity references other than &gt;/&lt;), then an optional closing &gt;, and
// HTML-unescaping exactly that span.
func UnescapeElement(s string, elements ...string) string {
	if len(elements) == 0 {
		return s
	}
	head := tagHeadRe(elements)
	var sb strings.Builder
	sb.Grow(len(s))
	i := 0
	for i < len(s) {
		// head is ^-anchored, so a non-nil match always begins at offset 0.
		loc := head.FindStringIndex(s[i:])
		if loc == nil {
			// No tag head starts here; emit one byte and advance.
			sb.WriteByte(s[i])
			i++
			continue
		}
		// The head matched "&lt;" (case-insensitive) plus '/'? plus the element
		// name, so loc[1] >= 5: the span is always non-empty and i advances.
		end := matchTagBody(s, i+loc[1])
		sb.WriteString(UnescapeHTML(s[i:end]))
		i = end
	}
	return sb.String()
}

// matchTagBody consumes, starting at j, the body part of an escaped tag —
// /(?>[^&]+|&(?![gl]t;)\w+;)*(?:&gt;)?/ — and returns the index just past it. A
// run of non-'&' bytes is consumed greedily; an entity reference is consumed
// only when it is "&" + word chars + ";" and is NOT "&gt;" or "&lt;". A trailing
// "&gt;" (the tag's own close) is then consumed once, optionally.
//
// The "&gt;"/"&lt;" comparisons are case-INsensitive (Ruby's /i flag governs the
// whole pattern, so "&GT;" closes the tag too). The span is later transformed by
// UnescapeHTML, whose entity table is case-sensitive, so an upper-cased "&GT;"
// stays literal even though it was part of the matched span — matching MRI's
// CGI.unescapeElement("&lt;A&GT;","A") == "<A&GT;".
func matchTagBody(s string, j int) int {
	for j < len(s) {
		if s[j] != '&' {
			j++
			continue
		}
		// At an '&'. If it is the tag-closing &gt; (or a &lt; that the Ruby
		// look-ahead also forbids inside the body), the body run stops.
		if hasPrefixFold(s[j:], "&gt;") || hasPrefixFold(s[j:], "&lt;") {
			break
		}
		// Otherwise accept "&" + \w+ + ";" as an in-body entity.
		k := j + 1
		for k < len(s) && isWord(s[k]) {
			k++
		}
		if k > j+1 && k < len(s) && s[k] == ';' {
			j = k + 1
			continue
		}
		// A bare '&' that is neither a forbidden head nor a \w+; entity ends
		// the body run (it cannot be consumed by either alternative).
		break
	}
	// Optional single trailing "&gt;" (case-insensitive, per Ruby's /i flag).
	if hasPrefixFold(s[j:], "&gt;") {
		j += len("&gt;")
	}
	return j
}

// hasPrefixFold reports whether s begins with prefix, comparing ASCII letters
// case-insensitively (prefix is always ASCII here).
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if lowerASCII(s[i]) != lowerASCII(prefix[i]) {
			return false
		}
	}
	return true
}

// lowerASCII lower-cases an ASCII letter, leaving every other byte unchanged.
func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// isWord reports whether b is a Ruby \w character (ASCII letter, digit, or '_').
func isWord(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}

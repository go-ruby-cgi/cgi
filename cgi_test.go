// Copyright (c) the go-ruby-cgi/cgi authors
//
// SPDX-License-Identifier: BSD-3-Clause

package cgi

import (
	"reflect"
	"testing"
)

// These deterministic, ruby-free golden tests pin every byte of the API against
// values captured from MRI 4.0.5 (ruby -rcgi). They alone hold coverage at 100%,
// so the no-ruby (Windows / qemu cross-arch) CI lanes still pass the gate.

func TestEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abcABC012", "abcABC012"}, // unreserved alnum unchanged
		{"-._~", "-._~"},           // unreserved marks unchanged
		{"a b", "a+b"},             // space -> '+'
		{"a+b", "a%2Bb"},           // literal '+' encoded
		{"&=/?:@", "%26%3D%2F%3F%3A%40"},
		{"a b+c&d=e/f?g~h.i_j-k*l", "a+b%2Bc%26d%3De%2Ff%3Fg~h.i_j-k%2Al"},
		{"ABCabc019 -_.!~*'()/:", "ABCabc019+-_.%21~%2A%27%28%29%2F%3A"},
		{"\x00\x7f\xff", "%00%7F%FF"}, // control + high byte
	}
	for _, c := range cases {
		if got := Escape(c.in); got != c.want {
			t.Errorf("Escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeURIComponent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abcABC012-._~", "abcABC012-._~"},
		{"a b", "a%20b"}, // space -> %20, not '+'
		{"a+b", "a%2Bb"},
		{"a b+c~d.e_f-g!h*i(j)", "a%20b%2Bc~d.e_f-g%21h%2Ai%28j%29"},
	}
	for _, c := range cases {
		if got := EscapeURIComponent(c.in); got != c.want {
			t.Errorf("EscapeURIComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnescape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"a+b", "a b"},
		{"a+b%20c", "a b c"},
		{"%26%3D", "&="},
		{"%E2%98%83", "☃"},
		{"%zz%2", "%zz%2"}, // malformed escapes left verbatim
		{"%2", "%2"},       // truncated at end
		{"%", "%"},
		{"%g0", "%g0"}, // invalid hi nibble
		{"%0g", "%0g"}, // invalid lo nibble
		{"a%26b", "a&b"},
	}
	for _, c := range cases {
		if got := Unescape(c.in); got != c.want {
			t.Errorf("Unescape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnescapeURIComponent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"a%20b+c", "a b+c"}, // '+' NOT decoded
		{"%2", "%2"},
		{"%zz", "%zz"},
		{"a%26b", "a&b"},
	}
	for _, c := range cases {
		if got := UnescapeURIComponent(c.in); got != c.want {
			t.Errorf("UnescapeURIComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain text", "plain text"},
		{`&<>"'`, "&amp;&lt;&gt;&quot;&#39;"},
		{`<a href="x">&'`, `&lt;a href=&quot;x&quot;&gt;&amp;&#39;`},
		// A dense block of the widest entity ('"' -> "&quot;") grows the output
		// to 6× the input, past the 2×len estimate, covering the append-grow path.
		{`""""`, "&quot;&quot;&quot;&quot;"},
		// Escapable byte only after a long safe prefix: the fast-path scan skips
		// the run, then the entity is spliced in.
		{"a long safe prefix then <", "a long safe prefix then &lt;"},
	}
	for _, c := range cases {
		if got := EscapeHTML(c.in); got != c.want {
			t.Errorf("EscapeHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnescapeHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"no amp here", "no amp here"},
		{"&amp;&lt;&gt;&quot;&apos;", `&<>"'`},
		{"&#39;&#x27;", "''"},
		{"&#65;&#x42;", "AB"},
		{"&#X41;", "A"},                          // uppercase X hex
		{"&#xff;", "ÿ"},                          // ÿ as UTF-8
		{"&#9731;", "☃"},                         // snowman (decimal)
		{"&#x2603;", "☃"},                        // snowman (hex)
		{"&#0;", "\x00"},                         // NUL
		{"&amp", "&amp"},                         // no ';' -> verbatim
		{"&;", "&;"},                             // empty entity body
		{"&#;", "&#;"},                           // empty number
		{"&#xZZ;", "&#xZZ;"},                     // non-hex
		{"&#x;", "&#x;"},                         // empty hex
		{"&#1a;", "&#1a;"},                       // non-digit in a decimal number
		{"&unknown;", "&unknown;"},               // unknown name
		{"&AMP;", "&AMP;"},                       // names are case-sensitive
		{"&nbsp;", "&nbsp;"},                     // not in the 5-name table
		{"&amp;amp;", "&amp;"},                   // only outer decodes
		{"&foo&amp;", "&foo&"},                   // resume after unmatched '&'
		{"&lt&gt;", "&lt>"},                      // first not terminated, second decodes
		{"&#1234567890123;", "&#1234567890123;"}, // overflow (decimal)
		{"&#x110000;", "&#x110000;"},             // overflow (hex)
		{"&#1114111;", "&#1114111;"},             // 0x10FFFF rejected
		{"&", "&"},                               // lone '&' at end
	}
	for _, c := range cases {
		if got := UnescapeHTML(c.in); got != c.want {
			t.Errorf("UnescapeHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestUnescapeHTMLCodepoints checks the raw UTF-8 byte layout produced for
// representative code points, including a surrogate (which Ruby encodes as a
// literal three-byte sequence rather than U+FFFD) and the inclusive maximum.
func TestUnescapeHTMLCodepoints(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"&#127;", []byte{127}},
		{"&#128;", []byte{194, 128}},
		{"&#2047;", []byte{223, 191}},
		{"&#2048;", []byte{224, 160, 128}},
		{"&#xD800;", []byte{237, 160, 128}}, // surrogate, raw 3-byte form
		{"&#65535;", []byte{239, 191, 191}},
		{"&#65536;", []byte{240, 144, 128, 128}},
		{"&#1114110;", []byte{244, 143, 191, 190}}, // 0x10FFFE, the max accepted
	}
	for _, c := range cases {
		if got := []byte(UnescapeHTML(c.in)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("UnescapeHTML(%q) bytes = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEscapeElement(t *testing.T) {
	cases := []struct {
		in       string
		elements []string
		want     string
	}{
		{`<BR><A HREF="url"></A>`, []string{"A"}, `<BR>&lt;A HREF=&quot;url&quot;&gt;&lt;/A&gt;`},
		{"<BR><A><IMG>", []string{"A", "IMG"}, "<BR>&lt;A&gt;&lt;IMG&gt;"},
		{"<Afoo>", []string{"A"}, "<Afoo>"},                   // \b prevents partial match
		{"<a><A>", []string{"A"}, "&lt;a&gt;&lt;A&gt;"},       // case-insensitive
		{"</A>", []string{"A"}, "&lt;/A&gt;"},                 // end tag
		{"<A", []string{"A"}, "&lt;A"},                        // optional '>'
		{"no tags", []string{"A"}, "no tags"},                 // nothing matches
		{"<A>", nil, "<A>"},                                   // no elements -> verbatim
		{`<A B="<x>">`, []string{"A"}, `&lt;A B=&quot;<x>">`}, // [^<>] stops at '<'
	}
	for _, c := range cases {
		if got := EscapeElement(c.in, c.elements...); got != c.want {
			t.Errorf("EscapeElement(%q, %v) = %q, want %q", c.in, c.elements, got, c.want)
		}
	}
}

func TestUnescapeElement(t *testing.T) {
	cases := []struct {
		in       string
		elements []string
		want     string
	}{
		{EscapeHTML(`<BR><A HREF="url"></A>`), []string{"A"}, `&lt;BR&gt;<A HREF="url"></A>`},
		{EscapeHTML(`<BR><A HREF="url"></A>`), []string{"A", "IMG"}, `&lt;BR&gt;<A HREF="url"></A>`},
		{"&lt;A&gt;&lt;B&gt;", []string{"A"}, "<A>&lt;B&gt;"},
		{"&lt;A B=&quot;x&amp;y&quot;&gt;", []string{"A"}, `<A B="x&y">`},
		{"&lt;A no semi", []string{"A"}, "<A no semi"},
		{"&lt;A &lt;nested&gt;", []string{"A"}, "<A &lt;nested&gt;"},
		{"&lt;A x&gt;y&gt;", []string{"A"}, "<A x>y&gt;"},
		{"&lt;Afoo&gt;", []string{"A"}, "&lt;Afoo&gt;"},         // \b prevents match
		{"&lt;/A&gt;", []string{"A"}, "</A>"},                   // end tag
		{"&lt;A&gt;", nil, "&lt;A&gt;"},                         // no elements -> verbatim
		{"plain", []string{"A"}, "plain"},                       // nothing matches
		{"&lt;Aa&amp;b&gt;", []string{"A"}, "&lt;Aa&amp;b&gt;"}, // \b after element prevents match
		// MRI applies /i to the whole pattern (so "&LT;"/"&GT;" match), but the
		// matched span is transformed by UnescapeHTML, whose entity table is
		// case-sensitive — so upper-cased entities inside the span stay literal.
		{"&LT;a&gt;", []string{"A"}, "&LT;a>"},           // span "&LT;a&gt;" -> "&LT;" kept, "&gt;" decoded
		{"&lt;A&GT;", []string{"A"}, "<A&GT;"},           // "&GT;" closes the span but stays literal
		{"&lt;a&gt;", []string{"A"}, "<a>"},              // element name IS case-insensitive
		{"&lt;A&amp&gt;", []string{"A"}, "<A&amp&gt;"},   // "&amp" lacks ';' -> body run ends, rest verbatim
		{"&lt;A&#65;&gt;", []string{"A"}, "<A&#65;&gt;"}, // '#' is not \w -> body run ends, rest verbatim
	}
	for _, c := range cases {
		if got := UnescapeElement(c.in, c.elements...); got != c.want {
			t.Errorf("UnescapeElement(%q, %v) = %q, want %q", c.in, c.elements, got, c.want)
		}
	}
}

func TestParseQuery(t *testing.T) {
	cases := []struct {
		in   string
		want map[string][]string
	}{
		{"", map[string][]string{}},
		{"a=1&b=2&a=3", map[string][]string{"a": {"1", "3"}, "b": {"2"}}},
		{"a=1;b=2", map[string][]string{"a": {"1"}, "b": {"2"}}}, // ';' separator
		{"x[]=1&x[]=2", map[string][]string{"x[]": {"1", "2"}}},  // brackets not special
		{"k", map[string][]string{"k": {}}},                      // bare key -> empty slice
		{"k=", map[string][]string{"k": {""}}},                   // '=' with empty value
		{"=v", map[string][]string{"": {"v"}}},                   // empty key kept
		{"a=1=2", map[string][]string{"a": {"1=2"}}},             // split on FIRST '='
		{"&&a=1&&", map[string][]string{"a": {"1"}}},             // empty pairs dropped
		{"a%20b=c+d&e=%26", map[string][]string{"a b": {"c d"}, "e": {"&"}}},
		{";", map[string][]string{}},
	}
	for _, c := range cases {
		if got := ParseQuery(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseQuery(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

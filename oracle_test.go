// Copyright (c) the go-ruby-cgi/cgi authors
//
// SPDX-License-Identifier: BSD-3-Clause

package cgi

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The differential MRI oracle drives the system `ruby` to compute the reference
// output of each CGI method over a corpus and compares it byte-for-byte with this
// package. It skips itself when `ruby` is absent (the qemu cross-arch lanes and
// the Windows lane, where the deterministic golden suite alone holds the 100%
// gate) and when the running MRI predates 4.0 (the version this library targets).
//
// Every oracle script binmodes both stdin and stdout so Windows text-mode never
// rewrites a byte. Records are length-prefixed ("<len>\n<bytes>") in both
// directions because a result may contain ANY byte — including NUL, which
// CGI.unescapeHTML("&#0;") produces — so no in-band separator is safe.

// rubyBin locates a usable `ruby` and confirms it is MRI >= 4.0; otherwise the
// oracle skips. The check runs the interpreter once and caches nothing — the
// suite is small.
func rubyBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	out, err := exec.Command(path, "-e",
		`$stdout.binmode; print(RUBY_VERSION >= "4.0" ? "ok" : "old")`).CombinedOutput()
	if err != nil {
		t.Skipf("ruby probe failed (%v); skipping MRI oracle", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		t.Skipf("ruby is %s; oracle requires MRI >= 4.0", strings.TrimSpace(string(out)))
	}
	return path
}

// rubyMap runs a Ruby script that reads length-prefixed inputs on stdin and
// prints one length-prefixed output record per input, and returns the decoded
// records. The script body is the expression mapping the variable `s` to its
// result; requireLine pulls in the CGI surface and the preamble binmodes both
// streams. Inputs arrive over a binary stream but are force_encoding-ed to UTF-8
// before the call, because CGI.unescapeHTML's numeric-entity decoding is
// encoding-sensitive (it only emits a multibyte char when the receiver is a
// Unicode string) and UTF-8 is the realistic, default encoding of Ruby source
// strings — the case this library reproduces.
func rubyMap(t *testing.T, bin, requireLine, expr string, inputs []string) []string {
	t.Helper()
	script := "$stdin.binmode; $stdout.binmode\n" +
		requireLine + "\n" +
		"results = []\n" +
		"loop do\n" +
		"  hdr = $stdin.gets(\"\\n\")\n" +
		"  break if hdr.nil?\n" +
		"  n = hdr.to_i\n" +
		"  s = (n.zero? ? \"\".b : $stdin.read(n)).force_encoding(\"UTF-8\")\n" +
		"  r = (" + expr + ").to_s.b\n" +
		"  results << r\n" +
		"end\n" +
		"results.each { |r| $stdout.write(r.bytesize.to_s + \"\\n\"); $stdout.write(r) }\n"
	cmd := exec.Command(bin, "-e", script)
	cmd.Stdin = strings.NewReader(frameAll(inputs))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby error: %v\nscript:\n%s\noutput:\n%s", err, script, out)
	}
	got, err := unframeAll(string(out), len(inputs))
	if err != nil {
		t.Fatalf("decoding ruby output: %v\nraw output:\n%q", err, out)
	}
	return got
}

// frameAll length-prefixes each input as "<bytesize>\n<bytes>".
func frameAll(inputs []string) string {
	var sb strings.Builder
	for _, in := range inputs {
		sb.WriteString(strconv.Itoa(len(in)))
		sb.WriteByte('\n')
		sb.WriteString(in)
	}
	return sb.String()
}

// unframeAll decodes exactly n length-prefixed records from the stream.
func unframeAll(stream string, n int) ([]string, error) {
	out := make([]string, 0, n)
	i := 0
	for k := 0; k < n; k++ {
		nl := strings.IndexByte(stream[i:], '\n')
		if nl < 0 {
			return nil, fmt.Errorf("record %d: missing length header", k)
		}
		length, err := strconv.Atoi(stream[i : i+nl])
		if err != nil {
			return nil, fmt.Errorf("record %d: bad length %q", k, stream[i:i+nl])
		}
		start := i + nl + 1
		if start+length > len(stream) {
			return nil, fmt.Errorf("record %d: truncated (need %d bytes)", k, length)
		}
		out = append(out, stream[start:start+length])
		i = start + length
	}
	return out, nil
}

// escapeCorpus is the shared input corpus for the escaping methods: ASCII edge
// cases, every separator and unreserved mark, spaces and pluses, multibyte UTF-8,
// and a control byte.
var escapeCorpus = []string{
	"", "abc", "a b", "a+b", "a b+c&d=e/f?g~h.i_j-k*l",
	"ABCabc019 -_.!~*'()/:", "&<>\"'", "100% sure", "café",
	"☃ snowman", "tab\tnl\n", "/path/to?x=1&y=2",
}

func TestOracleEscape(t *testing.T) {
	bin := rubyBin(t)
	want := rubyMap(t, bin, `require "cgi"`, "CGI.escape(s)", escapeCorpus)
	for i, in := range escapeCorpus {
		if got := Escape(in); got != want[i] {
			t.Errorf("Escape(%q) = %q, MRI = %q", in, got, want[i])
		}
	}
}

func TestOracleEscapeURIComponent(t *testing.T) {
	bin := rubyBin(t)
	want := rubyMap(t, bin, `require "cgi"`, "CGI.escapeURIComponent(s)", escapeCorpus)
	for i, in := range escapeCorpus {
		if got := EscapeURIComponent(in); got != want[i] {
			t.Errorf("EscapeURIComponent(%q) = %q, MRI = %q", in, got, want[i])
		}
	}
}

// unescapeCorpus exercises decode: valid escapes, '+', malformed/truncated '%',
// and round-tripped escapes.
var unescapeCorpus = []string{
	"", "abc", "a+b", "a+b%20c", "%26%3D", "%E2%98%83",
	"%zz%2", "%2", "%", "%g0", "%0g", "a%26b%2Bc",
}

func TestOracleUnescape(t *testing.T) {
	bin := rubyBin(t)
	want := rubyMap(t, bin, `require "cgi"`, "CGI.unescape(s)", unescapeCorpus)
	for i, in := range unescapeCorpus {
		if got := Unescape(in); got != want[i] {
			t.Errorf("Unescape(%q) = %q, MRI = %q", in, got, want[i])
		}
	}
}

func TestOracleUnescapeURIComponent(t *testing.T) {
	bin := rubyBin(t)
	want := rubyMap(t, bin, `require "cgi"`, "CGI.unescapeURIComponent(s)", unescapeCorpus)
	for i, in := range unescapeCorpus {
		if got := UnescapeURIComponent(in); got != want[i] {
			t.Errorf("UnescapeURIComponent(%q) = %q, MRI = %q", in, got, want[i])
		}
	}
}

func TestOracleEscapeHTML(t *testing.T) {
	bin := rubyBin(t)
	corpus := []string{"", "plain", "&<>\"'", `<a href="x">&'`, "a&b&c", "1<2>3"}
	want := rubyMap(t, bin, `require "cgi"`, "CGI.escapeHTML(s)", corpus)
	for i, in := range corpus {
		if got := EscapeHTML(in); got != want[i] {
			t.Errorf("EscapeHTML(%q) = %q, MRI = %q", in, got, want[i])
		}
	}
}

func TestOracleUnescapeHTML(t *testing.T) {
	bin := rubyBin(t)
	corpus := []string{
		"", "plain", "&amp;&lt;&gt;&quot;&apos;", "&#39;&#x27;",
		"&#65;&#x42;", "&#X41;", "&#xff;", "&#9731;", "&#x2603;", "&#0;",
		"&amp", "&;", "&#;", "&#xZZ;", "&#x;", "&#1a;", "&unknown;",
		"&AMP;", "&nbsp;", "&amp;amp;", "&foo&amp;", "&lt&gt;",
		"&#1234567890123;", "&#x110000;", "&#1114111;", "&#1114110;",
		"&#xD800;", "&", "mixed &amp; text &lt;ok&gt;",
	}
	want := rubyMap(t, bin, `require "cgi"`, "CGI.unescapeHTML(s)", corpus)
	for i, in := range corpus {
		if got := UnescapeHTML(in); got != want[i] {
			t.Errorf("UnescapeHTML(%q) = %q, MRI = %q", in, got, want[i])
		}
	}
}

// elementCases pair an input with the element names to (un)escape.
var elementCases = []struct {
	in       string
	elements []string
}{
	{`<BR><A HREF="url"></A>`, []string{"A"}},
	{"<BR><A><IMG>", []string{"A", "IMG"}},
	{"<Afoo>", []string{"A"}},
	{"<a><A>", []string{"A"}},
	{"</A>", []string{"A"}},
	{"<A", []string{"A"}},
	{"no tags", []string{"A"}},
	{`<A B="<x>">`, []string{"A"}},
}

func TestOracleEscapeElement(t *testing.T) {
	bin := rubyBin(t)
	// Each case has its own element list, so call MRI once per case with the list
	// spliced into the script as a literal array.
	for _, c := range elementCases {
		want := rubyMap(t, bin, `require "cgi"`,
			fmt.Sprintf("CGI.escapeElement(s, %s)", rubyArray(c.elements)), []string{c.in})
		if got := EscapeElement(c.in, c.elements...); got != want[0] {
			t.Errorf("EscapeElement(%q, %v) = %q, MRI = %q", c.in, c.elements, got, want[0])
		}
	}
}

// unescapeElementCases feed pre-escaped HTML so unescapeElement has tags to undo,
// plus the case-folding edge cases that pin the /i-vs-case-sensitive-table split.
var unescapeElementCases = []struct {
	in       string
	elements []string
}{
	{EscapeHTML(`<BR><A HREF="url"></A>`), []string{"A"}},
	{EscapeHTML(`<BR><A HREF="url"></A>`), []string{"A", "IMG"}},
	{"&lt;A&gt;&lt;B&gt;", []string{"A"}},
	{"&lt;A B=&quot;x&amp;y&quot;&gt;", []string{"A"}},
	{"&lt;A no semi", []string{"A"}},
	{"&lt;A &lt;nested&gt;", []string{"A"}},
	{"&lt;A x&gt;y&gt;", []string{"A"}},
	{"&lt;Afoo&gt;", []string{"A"}},
	{"&lt;/A&gt;", []string{"A"}},
	{"&LT;a&gt;", []string{"A"}},
	{"&lt;A&GT;", []string{"A"}},
	{"&lt;a&gt;", []string{"A"}},
	{"&lt;A&amp&gt;", []string{"A"}},
	{"&lt;A&#65;&gt;", []string{"A"}},
}

func TestOracleUnescapeElement(t *testing.T) {
	bin := rubyBin(t)
	for _, c := range unescapeElementCases {
		want := rubyMap(t, bin, `require "cgi"`,
			fmt.Sprintf("CGI.unescapeElement(s, %s)", rubyArray(c.elements)), []string{c.in})
		if got := UnescapeElement(c.in, c.elements...); got != want[0] {
			t.Errorf("UnescapeElement(%q, %v) = %q, MRI = %q", c.in, c.elements, got, want[0])
		}
	}
}

// TestOracleParseQuery checks CGI.parse. In MRI >= 4.0 CGI.parse lives in the
// separate `cgi` gem (it was removed from the default cgi/escape surface), so
// this test skips when that gem cannot be loaded. The map is compared via a
// canonical "k=>[v,...]" rendering printed by both sides.
func TestOracleParseQuery(t *testing.T) {
	bin := rubyBin(t)
	if out, err := exec.Command(bin, "-e",
		`begin; gem "cgi"; require "cgi"; print CGI.respond_to?(:parse); rescue LoadError; print false; end`,
	).CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Skip("CGI.parse unavailable (cgi gem not installed); skipping parse oracle")
	}
	corpus := []string{
		"", "a=1&b=2&a=3", "a=1;b=2", "x[]=1&x[]=2", "k", "k=", "=v",
		"a=1=2", "&&a=1&&", "a%20b=c+d&e=%26", ";", "a=%E2%98%83",
	}
	expr := "(_p = CGI.parse(s); _p.keys.sort.map { |k| k + \"=>\" + _p[k].inspect }.join(\"\\x01\"))"
	want := rubyMap(t, bin, `gem "cgi"; require "cgi"`, expr, corpus)
	for i, in := range corpus {
		if got := renderParsed(ParseQuery(in)); got != want[i] {
			t.Errorf("ParseQuery(%q) = %q, MRI = %q", in, got, want[i])
		}
	}
}

// renderParsed prints a parsed query in the same canonical form the Ruby oracle
// uses: keys sorted, each as `k=>["v1", "v2"]` joined by \x01.
func renderParsed(m map[string][]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		vals := make([]string, len(m[k]))
		for j, v := range m[k] {
			vals[j] = fmt.Sprintf("%q", v)
		}
		parts[i] = k + "=>[" + strings.Join(vals, ", ") + "]"
	}
	return strings.Join(parts, "\x01")
}

// rubyArray renders a Go string slice as a Ruby array literal of double-quoted
// strings (the element names here are plain ASCII identifiers, safe to quote
// directly).
func rubyArray(elements []string) string {
	quoted := make([]string, len(elements))
	for i, e := range elements {
		quoted[i] = fmt.Sprintf("%q", e)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

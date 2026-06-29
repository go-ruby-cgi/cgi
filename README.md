<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-cgi/brand/main/social/go-ruby-cgi-cgi.png" alt="go-ruby-cgi/cgi" width="720"></p>

# cgi — go-ruby-cgi

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-cgi.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of the escaping and query-parsing surface
of Ruby's [CGI](https://docs.ruby-lang.org/en/master/CGI.html) utility methods**
— the deterministic, interpreter-independent core of MRI 4.0.5's `CGI.escape` /
`CGI.unescape`, the HTML-entity helpers, the URI-component helpers, the element
helpers and `CGI.parse`. Every method matches the system `ruby` byte-for-byte,
**without any Ruby runtime**.

It is the CGI backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module with no dependency on the Ruby runtime — a
sibling of [go-ruby-yaml](https://github.com/go-ruby-yaml/yaml) (the Psych
emitter/loader), [go-ruby-regexp](https://github.com/go-ruby-regexp/regexp) (the
Onigmo engine) and [go-ruby-erb](https://github.com/go-ruby-erb/erb) (the ERB
compiler).

> **What it is — and isn't.** URL/form escaping, HTML-entity coding and query
> parsing are pure, deterministic string transforms that need **no interpreter**,
> so they live here as pure Go. The request/response cycle, cookies, multipart
> form handling and the HTML-generation DSL — everything that needs a live server
> or CGI environment — is **out of scope**; this library is the compute core only.
>
> **Ruby 4.0 note.** In MRI 4.0 the `cgi` library was slimmed to `cgi/escape`:
> `CGI.escape`, `CGI.unescape`, `CGI.escapeHTML`, `CGI.unescapeHTML`,
> `CGI.escapeURIComponent`, `CGI.unescapeURIComponent`, `CGI.escapeElement` and
> `CGI.unescapeElement` are the default surface, while `CGI.parse` now ships in
> the separate `cgi` gem. This package provides all of them (`ParseQuery` for
> `CGI.parse`).

## Features

Faithful port of the CGI utility methods, validated against the `ruby` binary on
every platform that has one:

- **`Escape` / `Unescape`** — `application/x-www-form-urlencoded`: every byte
  outside the unreserved set `A–Z a–z 0–9 - . _ ~` is percent-encoded (uppercase
  hex), a space becomes `+`, and `Unescape` decodes `+`→space and `%XX`, leaving
  malformed escapes verbatim (it never raises).
- **`EscapeURIComponent` / `UnescapeURIComponent`** — like the above but a space
  is `%20` (and `Unescape` does not treat `+` as a space).
- **`EscapeHTML` / `UnescapeHTML`** — encodes `& < > " '` (the apostrophe as
  `&#39;`); decoding recognises those five names plus `&apos;`, decimal `&#NN;`
  and hex `&#xHH;` / `&#XHH;` numeric entities (emitting the raw UTF-8 bytes of
  the code point, surrogates included, exactly as MRI does), and rejects unknown
  or overflowing entities verbatim.
- **`EscapeElement` / `UnescapeElement`** — HTML-(un)escape only the start/end
  tags of named elements, matching the element name case-insensitively at a word
  boundary.
- **`ParseQuery`** — `CGI.parse`: split on `&` and `;`, form-decode keys and
  values, accumulate repeated keys, and return an empty slice for a bare key.

CGO-free, dependency-free, **100% test coverage**, `gofmt` + `go vet` clean, and
green across the six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le,
s390x) and three operating systems (Linux, macOS, Windows).

## Install

```sh
go get github.com/go-ruby-cgi/cgi
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/go-ruby-cgi/cgi"
)

func main() {
	fmt.Println(cgi.Escape("a b&c"))                 // a+b%26c
	fmt.Println(cgi.Unescape("a+b%26c"))             // a b&c
	fmt.Println(cgi.EscapeURIComponent("a b"))       // a%20b
	fmt.Println(cgi.EscapeHTML(`<a href="x">&'`))    // &lt;a href=&quot;x&quot;&gt;&amp;&#39;
	fmt.Println(cgi.UnescapeHTML("&#9731; &amp;"))   // ☃ &
	fmt.Println(cgi.ParseQuery("a=1&b=2&a=3"))       // map[a:[1 3] b:[2]]
}
```

## API

```go
// Form (application/x-www-form-urlencoded) encoding — CGI.escape / CGI.unescape.
func Escape(s string) string
func Unescape(s string) string

// URI-component encoding — CGI.escapeURIComponent / CGI.unescapeURIComponent.
func EscapeURIComponent(s string) string
func UnescapeURIComponent(s string) string

// HTML-entity coding — CGI.escapeHTML / CGI.unescapeHTML.
func EscapeHTML(s string) string
func UnescapeHTML(s string) string

// Element-tag (un)escaping — CGI.escapeElement / CGI.unescapeElement.
func EscapeElement(s string, elements ...string) string
func UnescapeElement(s string, elements ...string) string

// Query parsing — CGI.parse.
func ParseQuery(query string) map[string][]string
```

## Tests & coverage

The suite pairs deterministic, ruby-free golden tests — which alone hold coverage
at 100%, so the qemu cross-arch and Windows lanes pass the gate — with a
**differential MRI oracle**: a corpus is run through the system `ruby`
(`CGI.escape`, `CGI.unescapeHTML`, `CGI.parse`, …) and compared byte-for-byte with
this package. The oracle binmodes both stdin and stdout so Windows text-mode never
rewrites a byte, gates on `RUBY_VERSION >= "4.0"`, and skips itself where `ruby`
is absent (and where the `cgi` gem is not installed, for the `CGI.parse` case).

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-cgi/cgi authors.

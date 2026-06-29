// Copyright (c) the go-ruby-cgi/cgi authors
//
// SPDX-License-Identifier: BSD-3-Clause

package cgi

import "strings"

// ParseQuery implements Ruby's CGI.parse(query). The query string is split on
// '&' and ';'; each pair is split on its first '='; key and value are
// Unescape-d (form decoding, so '+' is a space and "%XX" is decoded). Repeated
// keys accumulate their values in order. A pair with no '=' (e.g. "k") yields an
// empty value slice for that key. Ruby's CGI does NOT special-case "[]" in keys
// — the brackets are part of the key name verbatim.
//
//	ParseQuery("a=1&b=2&a=3") // => map[string][]string{"a":{"1","3"}, "b":{"2"}}
//	ParseQuery("x[]=1&x[]=2") // => map[string][]string{"x[]":{"1","2"}}
//	ParseQuery("k")           // => map[string][]string{"k":{}}
func ParseQuery(query string) map[string][]string {
	params := make(map[string][]string)
	for _, pair := range splitPairs(query) {
		key, value, hasValue := cut(pair, '=')
		key = Unescape(key)
		if _, seen := params[key]; !seen {
			// Mirror Ruby's `params[key] ||= []`: every key seen gets a slice,
			// even with no value. Use a non-nil empty slice so the zero-value
			// case ("k") round-trips as an explicit empty list.
			params[key] = []string{}
		}
		if hasValue {
			params[key] = append(params[key], Unescape(value))
		}
	}
	return params
}

// splitPairs splits query on '&' and ';' (Ruby's /[&;]/), dropping empty pairs.
// Ruby's CGI.parse splits the query with /[&;]/ and then runs
// `key, value = pair.split('=', 2)`; an empty pair makes key nil, which the
// `next unless key` guard discards, so an empty pair contributes nothing — which
// is exactly what skipping empty pairs here reproduces (e.g. "&&a=1&&" yields
// only {"a"=>["1"]}).
func splitPairs(query string) []string {
	if query == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '&' || query[i] == ';' {
			if i > start {
				out = append(out, query[start:i])
			}
			start = i + 1
		}
	}
	if start < len(query) {
		out = append(out, query[start:])
	}
	return out
}

// cut splits s on the first occurrence of sep, like strings.Cut. It returns the
// part before sep, the part after, and whether sep was present.
func cut(s string, sep byte) (before, after string, found bool) {
	if i := strings.IndexByte(s, sep); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

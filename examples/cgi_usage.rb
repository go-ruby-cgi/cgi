# frozen_string_literal: true

require "cgi"

# Form (application/x-www-form-urlencoded) escaping: space -> '+'.
puts CGI.escape("a b&c")              # => a+b%26c
puts CGI.unescape("a+b%26c")          # => a b&c

# URI-component escaping: space -> '%20'.
puts CGI.escapeURIComponent("a b")    # => a%20b
puts CGI.unescapeURIComponent("a%20b") # => a b

# HTML-entity coding of the five named entities (' as &#39;).
puts CGI.escapeHTML(%q{<a href="x">&'}) # => &lt;a href=&quot;x&quot;&gt;&amp;&#39;
puts CGI.unescapeHTML("&#9731; &amp;")  # => ☃ &

# Element-tag escaping: only the named element's tags are HTML-escaped.
puts CGI.escapeElement("<A HREF='x'>y</A> <b>", "A")
# => &lt;A HREF=&#39;x&#39;&gt;y&lt;/A&gt; <b>

# Query parsing: repeated keys accumulate, values are form-decoded.
p CGI.parse("a=1&b=2&a=3")            # => {"a"=>["1", "3"], "b"=>["2"]}

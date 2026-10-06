package normalize

import "testing"

func TestHTMLToText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text", "Hello world", "Hello world"},
		{"strips simple tags", "<p>Hello <b>world</b>!</p>", "Hello world!"},
		{"paragraph break", "<p>First</p><p>Second</p>", "First\n\nSecond"},
		{"list items", "<ul><li>One</li><li>Two</li></ul>", "One\nTwo"},
		{"br is a line break", "Line1<br>Line2<br/>Line3", "Line1\nLine2\nLine3"},
		{"entities unescaped", "Salt &amp; pepper &lt;3", "Salt & pepper <3"},
		{"nbsp becomes space", "a&nbsp;b", "a b"},
		{"script body dropped", "<p>keep</p><script>alert(1)</script><p>more</p>", "keep\n\nmore"},
		{"style body dropped", "<style>.x{color:red}</style><p>keep</p>", "keep"},
		{"double-escaped html (greenhouse)", "&lt;p&gt;Green&lt;b&gt;house&lt;/b&gt; &amp;amp; doors&lt;/p&gt;", "Greenhouse & doors"},
		{"whitespace collapsed", "  lots    of \t spaces  ", "lots of spaces"},
		{"links kept as text", `See <a href="http://x">docs</a> here`, "See docs here"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTMLToText(tc.in); got != tc.want {
				t.Errorf("HTMLToText(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestContentHash(t *testing.T) {
	// stable
	h1 := ContentHash("SRE", "Chicago, IL", "desc")
	h2 := ContentHash("SRE", "Chicago, IL", "desc")
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
	// title case-insensitive
	if ContentHash("sre", "Chicago, IL", "desc") != h1 {
		t.Error("hash should ignore title case")
	}
	// description change changes the hash
	if ContentHash("SRE", "Chicago, IL", "new desc") == h1 {
		t.Error("hash should change when description changes")
	}
	// location change changes the hash
	if ContentHash("SRE", "Austin, TX", "desc") == h1 {
		t.Error("hash should change when location changes")
	}
	// length
	if len(h1) != 64 {
		t.Errorf("hash should be 64 hex chars, got %d", len(h1))
	}
}

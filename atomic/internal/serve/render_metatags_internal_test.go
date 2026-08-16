package serve

import "testing"

// The wiki metadata block is not YAML frontmatter, so frontmatter.Parse leaves
// it; goldmark then drops the tags as unsafe raw HTML but keeps their text,
// surfacing as a stray "repo / e7f83d… / 1" paragraph above the page's title.
func TestStripLeadingMetaTags(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips a leading metadata block",
			in:   "<wiki-type>repo</wiki-type>\n<scan-sha>e7f83d79</scan-sha>\n<wiki-schema>1</wiki-schema>\n\n# Project signals\n",
			want: "# Project signals\n",
		},
		{
			name: "leaves a document with no metadata block untouched",
			in:   "# Title\n\nBody text.\n",
			want: "# Title\n\nBody text.\n",
		},
		// Only a leading run is stripped: the same tag further down is prose
		// or an example of the syntax, and belongs to the document.
		{
			name: "does not strip an identical tag further down the page",
			in:   "# Title\n\n<wiki-type>repo</wiki-type>\n",
			want: "# Title\n\n<wiki-type>repo</wiki-type>\n",
		},
		{
			name: "leaves a multi-line element alone",
			in:   "<wikis>\n- /a/wiki/index.md\n</wikis>\n\n# Title\n",
			want: "<wikis>\n- /a/wiki/index.md\n</wikis>\n\n# Title\n",
		},
		{
			name: "leaves an unclosed tag alone",
			in:   "<wiki-type>repo\n\n# Title\n",
			want: "<wiki-type>repo\n\n# Title\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(stripLeadingMetaTags([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("stripLeadingMetaTags:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

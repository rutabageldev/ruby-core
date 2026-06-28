//go:build fast

package gcal

import "testing"

// quoteEtag must produce the quoted entity-tag form an HTTP If-Match requires. The
// mirror stores etags trimmed of Google's quotes, so a bare value would 412 (issue #140).
func TestQuoteEtag(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"bare numeric", "3565350163851326", `"3565350163851326"`},
		{"already quoted", `"3565350163851326"`, `"3565350163851326"`},
		{"bare alnum", "p32o", `"p32o"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := quoteEtag(c.in); got != c.want {
				t.Errorf("quoteEtag(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

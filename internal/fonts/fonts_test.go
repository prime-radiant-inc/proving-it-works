package fonts

import (
	"slices"
	"testing"

	"golang.org/x/image/font/opentype"
)

func TestSansDrawsLatinButNotCJK(t *testing.T) {
	missing := Missing([]*opentype.Font{Sans()}, "Proving it works — λ 漢")
	if !slices.Equal(missing, []rune{'漢'}) {
		t.Fatalf("missing %q", missing)
	}
	if got := Describe(missing); got != "漢 (U+6F22)" {
		t.Fatalf("Describe: %q", got)
	}
}

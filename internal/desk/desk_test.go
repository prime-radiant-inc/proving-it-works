package desk

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"slices"
	"strings"
	"testing"
)

func picture(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 3, 2))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestSplitPicturesFromAPNGStream(t *testing.T) {
	a, b := picture(t, 10), picture(t, 200)
	stream := bytes.NewReader(append(append([]byte{}, a...), b...))
	r := newPictures(stream)
	for i, want := range [][]byte{a, b} {
		got, err := r.next()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("picture %d: %d bytes, %v", i, len(got), err)
		}
	}
	if _, err := r.next(); err != io.EOF {
		t.Fatalf("after the last picture: %v, want EOF", err)
	}
}

func TestATruncatedPictureIsAnError(t *testing.T) {
	a := picture(t, 10)
	r := newPictures(bytes.NewReader(a[:len(a)-5]))
	if _, err := r.next(); err == nil || err == io.EOF {
		t.Fatalf("got %v, want an error about a cut-off picture", err)
	}
	r = newPictures(strings.NewReader("not a png at all"))
	if _, err := r.next(); err == nil || !strings.Contains(err.Error(), "not a PNG") {
		t.Fatalf("got %v", err)
	}
}

func TestGlideMovesInFifteenStepsFromWhereThePointerIs(t *testing.T) {
	cmd := glide(image.Pt(0, 0), image.Pt(150, 30))
	var moves []string
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == "mousemove" {
			moves = append(moves, cmd[i+1]+","+cmd[i+2])
		}
	}
	if len(moves) != 15 || moves[0] != "10,2" || moves[14] != "150,30" {
		t.Fatalf("moves %v", moves)
	}
	if !slices.Contains(cmd, "sleep") {
		t.Fatal("a glide without pauses is a jump")
	}
}

func TestAClickHoversThenHoldsTheButton(t *testing.T) {
	got := strings.Join(press(3, false), " ")
	if got != "sleep 0.2 mousedown 3 sleep 0.12 mouseup 3" {
		t.Fatalf("got %q", got)
	}
	got = strings.Join(press(1, true), " ")
	if got != "sleep 0.2 mousedown 1 sleep 0.12 mouseup 1 sleep 0.08 mousedown 1 sleep 0.12 mouseup 1" {
		t.Fatalf("double click: %q", got)
	}
}

func TestCoordinatesAreRelativeToWhatIsFilmed(t *testing.T) {
	r := region{X: 100, Y: 50, W: 400, H: 300}
	if p, err := r.screen(10, 20); err != nil || p != image.Pt(110, 70) {
		t.Fatalf("got %v %v", p, err)
	}
	for _, bad := range [][2]int{{-1, 0}, {400, 0}, {0, 300}} {
		if _, err := r.screen(bad[0], bad[1]); err == nil || !strings.Contains(err.Error(), "outside the filmed 400x300") {
			t.Errorf("%v: got %v", bad, err)
		}
	}
}

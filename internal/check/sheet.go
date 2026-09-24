package check

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
)

// gridColumns picks a column count that fills the grid exactly where
// possible: an empty cell reads as a black frame, which is a defect signal,
// and a sheet that lies about the movie defeats the point of the sheet.
func gridColumns(n int) int {
	for _, c := range []int{4, 3, 5, 2} {
		if n%c == 0 {
			return c
		}
	}
	return min(4, n)
}

// contactSheet tiles up to 12 evenly spaced samples into out and returns the
// seconds they show.
func contactSheet(paths []string, out string) ([]int, error) {
	const count = 12
	var picks []int
	if len(paths) <= count {
		for i := range paths {
			picks = append(picks, i)
		}
	} else {
		for i := range count {
			picks = append(picks, int(math.RoundToEven(float64(i*(len(paths)-1))/float64(count-1))))
		}
	}
	var thumbs []image.Image
	for _, i := range picks {
		f, err := os.Open(paths[i])
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		thumbs = append(thumbs, img)
	}
	w, h := thumbs[0].Bounds().Dx(), thumbs[0].Bounds().Dy()
	cols := gridColumns(len(thumbs))
	rows := (len(thumbs) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cols*w, rows*h))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{48, 48, 52, 255}), image.Point{}, draw.Src)
	for i, t := range thumbs {
		at := image.Pt((i%cols)*w, (i/cols)*h)
		draw.Draw(sheet, image.Rectangle{at, at.Add(image.Pt(w, h))}, t, t.Bounds().Min, draw.Src)
	}
	f, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return picks, png.Encode(f, sheet)
}

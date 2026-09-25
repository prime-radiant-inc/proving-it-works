package desk

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// pictures splits a stream of PNG files, as ffmpeg's image2pipe writes them
// back to back, into one picture per PNG.
type pictures struct{ r *bufio.Reader }

func newPictures(r io.Reader) *pictures { return &pictures{bufio.NewReaderSize(r, 1<<20)} }

// next returns the next whole PNG, or io.EOF when the stream ended cleanly
// between pictures.
func (p *pictures) next() ([]byte, error) {
	var out bytes.Buffer
	sig := make([]byte, len(pngSignature))
	if _, err := io.ReadFull(p.r, sig); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("a picture was cut off: %w", err)
	}
	if !bytes.Equal(sig, pngSignature) {
		return nil, errors.New("the capture stream is not a PNG stream")
	}
	out.Write(sig)
	// chunks: length, type, data, CRC; the IEND chunk ends the picture
	head := make([]byte, 8)
	for {
		if _, err := io.ReadFull(p.r, head); err != nil {
			return nil, fmt.Errorf("a picture was cut off: %w", err)
		}
		out.Write(head)
		n := int64(binary.BigEndian.Uint32(head[:4])) + 4 // data and CRC
		if _, err := io.CopyN(&out, p.r, n); err != nil {
			return nil, fmt.Errorf("a picture was cut off: %w", err)
		}
		if string(head[4:]) == "IEND" {
			return out.Bytes(), nil
		}
	}
}

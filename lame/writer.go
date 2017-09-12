package lame

import (
	"io"
)

type LameWriter struct {
	output           io.Writer
	Encoder          *Encoder
	EncodedChunkSize int
}

func NewWriter(out io.Writer) *LameWriter {
	writer := &LameWriter{out, Init(), 0}
	return writer
}

func (lw *LameWriter) Write(p []byte) (int, error) {
	out := lw.Encoder.Encode(p)
	lw.EncodedChunkSize = len(out)

	if lw.EncodedChunkSize > 0 {
		_, err := lw.output.Write(out)
		if err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

func (lw *LameWriter) Close() (int, error) {
	out := lw.Encoder.Flush()
	padding := lw.Encoder.GetPadding()
	if len(out) == 0 {
		return padding, nil
	}
	_, err := lw.output.Write(out)
	return padding, err
}

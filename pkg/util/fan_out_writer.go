package util

import "io"

type FanOutWriter struct {
	writers []io.Writer
}

func NewFanOutWriter(writers ...io.Writer) *FanOutWriter {
	return &FanOutWriter{writers: writers}
}

func (fow *FanOutWriter) Write(p []byte) (n int, err error) {
	for _, w := range fow.writers {
		n, err = w.Write(p)
		if err != nil {
			return n, err
		}
	}
	return n, err
}

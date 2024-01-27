package xgolib

import "log"

type logWriter struct {
	logger *log.Logger
}

func newLogWriter(l *log.Logger) logWriter {
	lw := logWriter{}
	lw.logger = l
	return lw
}

func (lw logWriter) Write(p []byte) (n int, err error) {
	lw.logger.Println(p)
	return len(p), nil
}

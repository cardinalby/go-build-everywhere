package util

type logger interface {
	Print(v ...interface{})
}

type LogWriter struct {
	logger logger
}

func NewLogWriter(l logger) LogWriter {
	lw := LogWriter{}
	lw.logger = l
	return lw
}

func (lw LogWriter) Write(p []byte) (n int, err error) {
	lw.logger.Print(p)
	return len(p), nil
}

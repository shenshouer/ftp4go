package ftp4go

import (
	"bufio"
	"fmt"
)

// A Writer implements convenience methods for writing
// requests or responses to a text protocol network connection.
type Writer struct {
	W   *bufio.Writer
	//dot *dotWriter
}

// NewWriter returns a new Writer writing to w.
func NewWriter(w *bufio.Writer) *Writer {
	return &Writer{W: w}
}

var crnl = []byte{'\r', '\n'}

// PrintfLine writes the formatted output followed by \r\n.
func (w *Writer) PrintfLine(format string, args ...interface{}) error {
	fmt.Fprintf(w.W, format, args...)
	w.W.Write(crnl)
	return w.W.Flush()
}

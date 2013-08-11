package ftp4go

import (
	"bufio"
	"bytes"
	"testing"
)

func TestPrintfLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(bufio.NewWriter(&buf))
	err := w.PrintfLine("foo %d", 123)
	if s := buf.String(); s != "foo 123\r\n" || err != nil {
		t.Fatalf("s=%q; err=%s", s, err)
	}
}

package ftp4go

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	BYTE_BLK = 1024
)

var (
	NewErrReply = func(error error) error { return errors.New("Reply error: " + error.Error()) }
	NewErrTemp  = func(error error) error { return errors.New("Temporary error: " + error.Error()) }
	NewErrPerm  = func(error error) error { return errors.New("Permanent error: " + error.Error()) }
	NewErrProto = func(error error) error { return errors.New("Protocol error: " + error.Error()) }
)

// string writer
type stringSliceWriter struct {
	s []string
}

// utility string writer
func (sw *stringSliceWriter) Write(p []byte) (n int, err error) {
	sw.s = append(sw.s, string(p))
	n = len(p)
	return
}

// string writer
type textFileWriter struct {
	//file *os.File
	bw *bufio.Writer
}

func newTextFileWriter(f *os.File) *textFileWriter {
	return &textFileWriter{bufio.NewWriter(f)}
}

// utility string writer
func (tfw *textFileWriter) Write(p []byte) (n int, err error) {
	//return fmt.Fprintln(tfw.f, string(p))
	n, err = tfw.bw.Write(p)
	if err != nil {
		return
	}

	n1, err1 := tfw.bw.WriteRune('\n') // always add a new line
	return n + n1, err1
}

type CallbackInfo struct {
	Resourcename     string
	Filename         string
	BytesTransmitted int64
	Eof              bool
}

type Callback func(info *CallbackInfo)

type Response struct {
	Code    int
	Message string
	Stream  []byte
}

func (r *Response) getFirstChar() string {
	if r == nil {
		return ""
	}
	return strconv.Itoa(r.Code)[0:1]
}

var re227, re150 *regexp.Regexp

func init() {
	re227, _ = regexp.Compile("([0-9]+),([0-9]+),([0-9]+),([0-9]+),([0-9]+),([0-9]+)")
	re150, _ = regexp.Compile("150 .* \\(([0-9]+) bytes\\)")
}

// Dial connects to the given address on the given network using net.Dial
// and then returns a new Conn for the connection.
func Dial(network, addr string) (net.Conn, error) {
	c, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (ftp *FTP) NewConn(addr string) error {

	c, err := ftp.dialer.Dial("tcp", addr)

	if err != nil {
		return err
	}

	// use textproto for parsing
	ftp.conn = c
	ftp.textprotoConn = textproto.NewConn(c)
	return nil
}

// SendAndRead sends a command to the server and reads the response.
func (ftp *FTP) SendAndRead(cmd FtpCmd, params ...string) (response *Response, err error) {
	if err = ftp.Send(cmd, params...); err != nil {
		return nil, err
	}
	return ftp.Read(cmd)
}

// Send sends a command to the server.
func (ftp *FTP) Send(cmd FtpCmd, params ...string) (err error) {
	fullCmd := cmd.String()
	//ftp.writeInfo(fmt.Sprintf("Sending to server partial command '%s'", fullCmd))
	if len(params) > 0 {
		fullCmd = cmd.AppendParameters(params...)
	}

	ftp.writeInfo(fmt.Sprintf("Sending to server command '%s'", fullCmd))
	//_, err = ftp.textprotoConn.Cmd(fullCmd)
	err = ftp.textprotoConn.PrintfLine(fullCmd)

	return
}

// Read reads the response along with the response code from the server
func (ftp *FTP) Read(cmd FtpCmd) (resp *Response, err error) {

	var msg string
	var code int

	if code, msg, err = ftp.textprotoConn.ReadResponse(-1); err != nil {
		return nil, err
	}

	ftp.writeInfo(fmt.Sprintf("The message returned by the server was: code=%d, message=%s", code, msg))

	c := strconv.Itoa(code)[0:1]

	switch {
	//valid
	case strings.IndexAny(c, "123") >= 0:
		return &Response{Code: code, Message: msg}, nil
	//wrong
	case c == "4":
		err = errors.New("Temporary error: " + msg)
	case c == "5":
		err = errors.New("Permanent error: " + msg)
	default:
		err = errors.New("Protocol error: " + msg)
	}

	ftp.writeInfo("Response error")
	return nil, err
}

// parse227 parses the 227 response for PASV request.
// Raises a protocol error if it does not contain {h1,h2,h3,h4,p1,p2}.
// Returns the host and port.
func parse227(resp *Response) (host string, port int, err error) {
	if resp.Code != 227 {
		err = NewErrProto(errors.New(resp.Message))
		return
	}

	matches := re227.FindStringSubmatch(resp.Message)
	if matches == nil {
		err = NewErrProto(errors.New("No matching pattern for message:" + resp.Message))
		return
	}
	numbers := matches[1:] // get the groups
	host = strings.Join(numbers[:4], ".")
	p1, _ := strconv.Atoi(numbers[4])
	p2, _ := strconv.Atoi(numbers[5])
	port = (p1 << 8) + p2
	return
}

// parse150ForSize parses the '150' response for a RETR request.
// Returns the expected transfer size or None; size is not guaranteed to
// be present in the 150 message.
func parse150ForSize(resp *Response) (int, error) {
	if resp.Code != 150 {
		return -1, NewErrReply(errors.New(resp.Message))
	}

	matches := re150.FindStringSubmatch(resp.Message)
	if len(matches) < 2 {
		return -1, nil
	}

	return strconv.Atoi(string(matches[1]))

}

// parse257 parses the 257 response for a MKD or PWD request, the response is a directory name.
// Return the directory name in the 257 reply.
func parse257(resp *Response) (dirname string, err error) {
	if resp.Code != 257 {
		err = NewErrProto(errors.New(resp.Message))
		return "", err
	}
	if resp.Message[3:5] != " \"" {
		return "", nil // Not compliant to RFC 959, but UNIX ftpd does this
	}
	dirname = ""
	i := 5
	n := len(resp.Message)
	for i < n {
		c := resp.Message[i]
		i++
		if c == '"' {
			if i >= n || resp.Message[i] != '"' {
				break
			}
			i++
		}
		dirname = dirname + string(c)
	}
	return dirname, nil
}

// parse211 parses the 211 response for a FEAT command.
// Return the list of feats.
func parse211(resp *Response) (list []string, err error) {
	if resp.Code != 211 {
		err = NewErrProto(errors.New(resp.Message))
		return nil, err
	}

	list = make([]string, 0, 20)
	var no int

	r := bufio.NewReader(strings.NewReader(resp.Message))

	for {
		line, _, err1 := r.ReadLine()

		if err1 != nil {
			if err1 == io.EOF {
				break
			}
			return list, err1
		}

		l := strings.TrimSpace(string(line))

		if !strings.HasPrefix(l, strconv.Itoa(resp.Code)) && len(l) > 0 {
			list = append(list, l)
			no++
		}
	}
	return list[:no], nil

}

// TrimString returns s without leading and trailing ASCII space.
func TrimString(s string) string {
	for len(s) > 0 && isASCIISpace(s[0]) {
		s = s[1:]
	}
	for len(s) > 0 && isASCIISpace(s[len(s)-1]) {
		s = s[:len(s)-1]
	}
	return s
}

// TrimBytes returns b without leading and trailing ASCII space.
func TrimBytes(b []byte) []byte {
	for len(b) > 0 && isASCIISpace(b[0]) {
		b = b[1:]
	}
	for len(b) > 0 && isASCIISpace(b[len(b)-1]) {
		b = b[:len(b)-1]
	}
	return b
}

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isASCIILetter(b byte) bool {
	b |= 0x20 // make lower case
	return 'a' <= b && b <= 'z'
}

// An Error represents a numeric error response from a server.
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%03d %s", e.Code, e.Msg)
}

// A ProtocolError describes a protocol violation such
// as an invalid response or a hung-up connection.
type ProtocolError string

func (p ProtocolError) Error() string {
	return string(p)
}

package ftp4go

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"regexp"
	"net"
	"net/textproto"
	"fmt"
)

const (
	BYTE_BLK = 1024
)

var (
	NewErrReply = func(error os.Error) os.Error { return os.NewError("Reply error: " + error.String()) }
	NewErrTemp  = func(error os.Error) os.Error { return os.NewError("Temporary error: " + error.String()) }
	NewErrPerm  = func(error os.Error) os.Error { return os.NewError("Permanent error: " + error.String()) }
	NewErrProto = func(error os.Error) os.Error { return os.NewError("Protocol error: " + error.String()) }
)

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

// A Reader implements convenience methods for reading requests
// or responses from a text protocol network connection.
type FtpReader struct {
	R *textproto.Reader //*bufio.Reader
}

// NewReader returns a new Reader reading from r.
func NewFtpReader(conn net.Conn) *FtpReader {
	return &FtpReader{R: textproto.NewReader(bufio.NewReader(conn))}
}

// readMultiLine gets a response which may possibly consist of multiple lines. 
// Return a single string with no trailing CRLF. If the response consists of multiple
// lines these are separated by "\n" characters in the string.
func (reader *FtpReader) readMultiLine() (text string, err os.Error) {
	var line string
	if line, err = reader.readLine(); err != nil {
		if err != os.EOF {
			return line, err
		}
	}

	if line[3:4] == "-" {
		for code := line[:3]; ; {
			var nextline string
			if nextline, err = reader.readLine(); err != nil {
				if err != os.EOF {
					return line, err
				}
			}
			line = line + "\n" + nextline
			if nextline[:3] == code && nextline[3:4] != "-" {
				break
			}
			if err == os.EOF {
				break
			}
		}
	}
	return line, nil
}

// readLine returns one line from the server stripping CRLF.
// Return an error if the connection fails.
// 
// NOTE:
// the encoding is always unicode.
func (reader *FtpReader) readLine() (line string, err os.Error) {
	return reader.R.ReadLine()
}

// SendAndRead sends a command to the server and reads the response.
func (ftp *FTP) SendAndRead(cmd FtpCmd, params ...string) (response *Response, err os.Error) {
	if err = ftp.Send(cmd, params...); err != nil {
		return nil, err
	}
	return ftp.Read(cmd)
}

// SendAndReadEmpty sends a command to the server and reads the response accepting only "empty" responses.
func (ftp *FTP) SendAndReadEmpty(cmd FtpCmd, params ...string) (response *Response, err os.Error) {
	if err = ftp.Send(cmd, params...); err != nil {
		return
	}
	return ftp.ReadEmpty(cmd)
}

// Send sends a command to the server.
func (ftp *FTP) Send(cmd FtpCmd, params ...string) (err os.Error) {
	fullCmd := cmd.String()
	ftp.writeInfo(fmt.Sprintf("Sending to server partial command '%s'", fullCmd))
	if len(params) > 0 {
		fullCmd = cmd.AppendParameters(params...)
	}

	ftp.writeInfo(fmt.Sprintf("Sending to server command '%s'", fullCmd))
	//_, err = ftp.textprotoConn.Cmd(fullCmd)
	_, err = ftp.conn.Write([]byte(fullCmd + CRLF))

	return
}

// ReadEmpty reads the response along with the response code from the server and 
// expects a response beginning with code "2". It returns an error otherwise.
func (ftp *FTP) ReadEmpty(cmd FtpCmd) (resp *Response, err os.Error) {

	resp, err = ftp.Read(cmd)

	if err != nil {
		return
	}

	if c := resp.Message[:1]; c != "2" {
		err = NewErrReply(os.NewError(resp.Message))
		resp = nil
	}
	return

}

// Read reads the response along with the response code from the server
func (ftp *FTP) Read(cmd FtpCmd) (resp *Response, err os.Error) {

	reader := NewFtpReader(ftp.conn)

	var msg string
	if msg, err = reader.readMultiLine(); err != nil {
		return nil, err
	}

	ftp.writeInfo("The message returned by the server was:", msg)

	code, _ := strconv.Atoi(msg[:3])

	switch c := msg[:1]; true {
	//valid
	case strings.IndexAny(c, "123") >= 0:
		return &Response{Code: code, Message: msg}, nil
	//wrong
	case c == "4":
		err = os.NewError("Temporary error: " + msg)
	case c == "5":
		err = os.NewError("Permanent error: " + msg)
	default:
		err = os.NewError("Protocol error: " + msg)
	}

	return nil, err
}

// parse227 parses the 227 response for PASV request.
// Raises a protocol error if it does not contain {h1,h2,h3,h4,p1,p2}.
// Returns the host and port.
func parse227(resp *Response) (host string, port int, err os.Error) {
	if resp.Code != 227 {
		err = NewErrProto(os.NewError(resp.Message))
		return
	}

	re, _ := regexp.Compile("([0-9]+),([0-9]+),([0-9]+),([0-9]+),([0-9]+),([0-9]+)")
	matches := re.FindStringSubmatch(resp.Message)
	if matches == nil {
		err = NewErrProto(os.NewError("No matching pattern for message:" + resp.Message))
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
func parse150ForSize(resp *Response) (int, os.Error) {
	if resp.Code != 150 {
		return -1, NewErrReply(os.NewError(resp.Message))
	}

	re, _ := regexp.Compile("150 .* \\(([0-9]+) bytes\\)")
	matches := re.FindStringSubmatch(resp.Message)
	if len(matches) < 2 {
		return -1, nil
	}

	//print("The match from parse150ForSize returned: " + matches[1] + "\n")

	return strconv.Atoi(string(matches[1]))

}

// parse257 parses the 257 response for a MKD or PWD request, the response is a directory name.
// Return the directory name in the 257 reply.
func parse257(resp *Response) (dirname string, err os.Error) {
	if resp.Code != 257 {
		err = NewErrProto(os.NewError(resp.Message))
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
func parse211(resp *Response) (list []string, err os.Error) {
	if resp.Code != 211 {
		err = NewErrProto(os.NewError(resp.Message))
		return nil, err
	}

	list = make([]string, 0, 20)
	var no int

	r := bufio.NewReader(strings.NewReader(resp.Message))

	for {
		line, _, err := r.ReadLine()
		l := strings.TrimSpace(string(line))

		if !strings.HasPrefix(l, strconv.Itoa(resp.Code)) && len(l) > 0 {
			list = append(list, l)
			no++
		}
		if err != nil {
			if err == os.EOF {
				break
			}
			return
		}
	}
	return list[:no], nil

}

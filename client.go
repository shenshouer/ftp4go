// Package ftp implements an FTP client.
package ftp4go

import (
	"bufio"
	"code.google.com/p/go.net/proxy"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// The default constants
const (
	DefaultFtpPort       = 21
	DefaultTimeoutInMsec = 1000
	CRLF                 = "\r\n"
	BLOCK_SIZE           = 8192
)

// FTP command strings
type FtpCmd int

const (
	NONE_FTP_CMD       FtpCmd = 0
	USER_FTP_CMD       FtpCmd = 1
	PASSWORD_FTP_CMD   FtpCmd = 2
	ACCT_FTP_CMD       FtpCmd = 3
	ABORT_FTP_CMD      FtpCmd = 4
	PORT_FTP_CMD       FtpCmd = 5
	PASV_FTP_CMD       FtpCmd = 6
	TYPE_A_FTP_CMD     FtpCmd = 7
	NLST_FTP_CMD       FtpCmd = 8
	LIST_FTP_CMD       FtpCmd = 9
	FEAT_FTP_CMD       FtpCmd = 10
	OPTS_FTP_CMD       FtpCmd = 11
	RETR_FTP_CMD       FtpCmd = 12
	TYPE_I_FTP_CMD     FtpCmd = 13
	STORE_FTP_CMD      FtpCmd = 14
	RENAMEFROM_FTP_CMD FtpCmd = 15
	RENAMETO_FTP_CMD   FtpCmd = 16
	DELETE_FTP_CMD     FtpCmd = 17
	CWD_FTP_CMD        FtpCmd = 18
	SIZE_FTP_CMD       FtpCmd = 19
	MKDIR_FTP_CMD      FtpCmd = 20
	RMDIR_FTP_CMD      FtpCmd = 21
	PWDIR_FTP_CMD      FtpCmd = 22
	CDUP_FTP_CMD       FtpCmd = 23
	QUIT_FTP_CMD       FtpCmd = 24
	MLSD_FTP_CMD       FtpCmd = 25
)

const MSG_OOB = 0x1 //Process data out of band

var ftpCmdStrings = map[FtpCmd]string{
	NONE_FTP_CMD:       "",
	USER_FTP_CMD:       "USER",
	PASSWORD_FTP_CMD:   "PASS",
	ACCT_FTP_CMD:       "ACCT",
	ABORT_FTP_CMD:      "ABOR",
	PORT_FTP_CMD:       "PORT",
	PASV_FTP_CMD:       "PASV",
	TYPE_A_FTP_CMD:     "TYPE A",
	NLST_FTP_CMD:       "NLST",
	LIST_FTP_CMD:       "LIST",
	MLSD_FTP_CMD:       "MLSD",
	FEAT_FTP_CMD:       "FEAT",
	OPTS_FTP_CMD:       "OPTS",
	RETR_FTP_CMD:       "RETR",
	TYPE_I_FTP_CMD:     "TYPE I",
	STORE_FTP_CMD:      "STOR",
	RENAMEFROM_FTP_CMD: "RNFR",
	RENAMETO_FTP_CMD:   "RNTO",
	DELETE_FTP_CMD:     "DELE",
	CWD_FTP_CMD:        "CWD",
	SIZE_FTP_CMD:       "SIZE",
	MKDIR_FTP_CMD:      "MKD",
	RMDIR_FTP_CMD:      "RMD",
	PWDIR_FTP_CMD:      "PWD",
	CDUP_FTP_CMD:       "CDUP",
	QUIT_FTP_CMD:       "QUIT",
}

// The FTP client structure containing:
// - host, user, password, acct, timeout
type FTP struct {
	debugging     int
	Host          string
	Port          int
	file          string
	welcome       string
	passiveserver bool
	logger        *log.Logger
	//timeoutInMsec int64
	textprotoConn *textproto.Conn
	dialer        proxy.Dialer
	conn          net.Conn
	encoding      string
}

type NameFactsLine struct {
	Name  string
	Facts map[string]string
}

func getTimeoutInMsec(msec int64) time.Time {
	return time.Now().Add(time.Duration(msec) * time.Millisecond)
}

func (i FtpCmd) String() string {
	if cmd, ok := ftpCmdStrings[i]; ok {
		return cmd
	}
	panic("No cmd found")
}

func (i FtpCmd) AppendParameters(pars ...string) string {
	allPars := make([]string, len(pars)+1)
	allPars[0] = i.String()
	var k int = 1
	for _, par := range pars {
		if p := strings.TrimSpace(par); len(p) > 0 {
			allPars[k] = p
			k++
		}
	}
	return strings.Join(allPars[:k], " ")
}

func (ftp *FTP) writeInfo(params ...interface{}) {
	if ftp.debugging >= 1 {
		log.Println(params...)
	}
}

// NewFTP creates a new FTP client using a debug level, default is 0, which is disabled.
// The FTP server uses the passive tranfer mode by default.
//
// 	Debuglevel:
// 		0 -> disabled
// 		1 -> information
// 		2 -> verbose
//
func NewFTP(debuglevel int) *FTP {
	logger := log.New(os.Stdout, "", log.LstdFlags) //syslog.NewLogger(syslog.LOG_ERR, 999)
	ftp := &FTP{
		debugging: debuglevel,
		Port:      DefaultFtpPort,
		logger:    logger,
		//timeoutInMsec: DefaultTimeoutInMsec,
		passiveserver: true,
	}
	return ftp
}

// Connect connects to the host by using the specified port or the default one if the value is <=0.
func (ftp *FTP) Connect(host string, port int, socks5ProxyUrl string) (resp *Response, err error) {

	if len(host) == 0 {
		return nil, errors.New("The host must be specified")
	}
	ftp.Host = host

	if port <= 0 {
		port = DefaultFtpPort
	}

	addr := fmt.Sprintf("%s:%d", ftp.Host, ftp.Port)

	// use the system proxy if emtpy
	if socks5ProxyUrl == "" {
		ftp.writeInfo("using environment proxy, url: ", os.Getenv("all_proxy"))
		ftp.dialer = proxy.FromEnvironment()
	} else {
		ftp.dialer = proxy.Direct

		if u, err1 := url.Parse(socks5ProxyUrl); err1 == nil {
			p, err2 := proxy.FromURL(u, proxy.Direct)
			if err2 == nil {
				ftp.dialer = p
			}
		}

	}

	err = ftp.NewConn(addr)
	if err != nil {
		return
	}

	ftp.writeInfo("host:", ftp.Host, " port:", strconv.Itoa(ftp.Port), " proxy enabled:", ftp.dialer != proxy.Direct)

	// NOTE: this is an absolute time that needs refreshing after each READ/WRITE net operation
	//ftp.conn.conn.SetDeadline(getTimeoutInMsec(ftp.timeoutInMsec))

	if resp, err = ftp.Read(NONE_FTP_CMD); err != nil {
		return
	}
	ftp.welcome = resp.Message
	ftp.writeInfo("Successfully connected on local address:", ftp.conn.LocalAddr())
	return
}

// SetPassive sets the mode to passive or active for data transfers.
// With a false statement use the normal PORT mode.
// With a true statement use the PASV command.
func (ftp *FTP) SetPassive(ispassive bool) {
	ftp.passiveserver = ispassive
}

// Login logs on to the server.
func (ftp *FTP) Login(username, password string, acct string) (response *Response, err error) {

	//Login, default anonymous.
	if len(username) == 0 {
		username = "anonymous"
	}
	if len(password) == 0 {
		password = ""
	}

	if username == "anonymous" && len(password) == 0 {
		// If there is no anonymous ftp password specified
		// then we'll just use anonymous@
		// We don't send any other thing because:
		// - We want to remain anonymous
		// - We want to stop SPAM
		// - We don't want to let ftp sites to discriminate by the user,
		//   host or country.
		password = password + "anonymous@"
	}

	ftp.writeInfo("username:", username)
	tempResponse, err := ftp.SendAndRead(USER_FTP_CMD, username)
	if err != nil {
		return
	}

	if tempResponse.getFirstChar() == "3" {
		tempResponse, err = ftp.SendAndRead(PASSWORD_FTP_CMD, password)
		if err != nil {
			return
		}
	}
	if tempResponse.getFirstChar() == "3" {
		tempResponse, err = ftp.SendAndRead(ACCT_FTP_CMD, acct)
		if err != nil {
			return
		}
	}
	if tempResponse.getFirstChar() != "2" {
		err = NewErrReply(errors.New(tempResponse.Message))
		return
	}
	return tempResponse, err
}

// Abort interrupts a file transfer, which uses out-of-band data.
// This does not follow the procedure from the RFC to send Telnet IP and Synch;
// that does not seem to work with all servers. Instead just send the ABOR command as OOB data.
func (ftp *FTP) Abort() (response *Response, err error) {
	return ftp.SendAndRead(ABORT_FTP_CMD)
}

// SendPort sends a PORT command with the current host and given port number
func (ftp *FTP) SendPort(host string, port int) (response *Response, err error) {
	hbytes := strings.Split(host, ".") // return all substrings
	pbytes := []string{strconv.Itoa(port / 256), strconv.Itoa(port % 256)}
	bytes := strings.Join(append(hbytes, pbytes...), ",")
	return ftp.SendAndRead(PORT_FTP_CMD, bytes)
}

// makePasv sends a PASV command and returns the host and port number to be used for the data transfer connection.
func (ftp *FTP) makePasv() (host string, port int, err error) {
	var resp *Response
	resp, err = ftp.SendAndRead(PASV_FTP_CMD)
	if err != nil {
		return
	}
	return parse227(resp)
}

// Acct sends an ACCT command.
func (ftp *FTP) Acct() (response *Response, err error) {
	return ftp.SendAndRead(ACCT_FTP_CMD)
}

// Mlsd lists a directory in a standardized format by using MLSD
// command (RFC-3659). If path is omitted the current directory
// is assumed. "facts" is a list of strings representing the type
// of information desired (e.g. ["type", "size", "perm"]).
// Return a generator object yielding a tuple of two elements
// for every file found in path.
// First element is the file name, the second one is a dictionary
// including a variable number of "facts" depending on the server
// and whether "facts" argument has been provided.
func (ftp *FTP) Mlsd(path string, facts []string) (ls []*NameFactsLine, err error) {

	if len(facts) > 0 {
		if _, err = ftp.Opts("MLST", strings.Join(facts, ";")+";"); err != nil {
			return nil, err
		}
	}

	sw := &stringSliceWriter{make([]string, 0, 50)}
	if err = ftp.GetLines(MLSD_FTP_CMD, sw, path); err != nil {
		return nil, err
	}

	ls = make([]*NameFactsLine, len(sw.s))
	for _, l := range sw.s {
		tkns := strings.Split(strings.TrimSpace(l), " ")
		name := tkns[0]
		facts := strings.Split(tkns[1], ";")
		ftp.writeInfo("Found facts:", facts)
		vals := make(map[string]string, len(facts)-1)
		for i := 0; i < len(facts)-1; i++ {
			fpair := strings.Split(facts[i], "=")
			vals[fpair[0]] = fpair[1]
		}
		ls = append(ls, &NameFactsLine{strings.ToLower(name), vals})
	}
	return
}

// Feat lists all new FTP features that the server supports beyond those described in RFC 959.
func (ftp *FTP) Feat(params ...string) (fts []string, err error) {
	var r *Response
	if r, err = ftp.SendAndRead(FEAT_FTP_CMD); err != nil {
		return
	}

	return parse211(r)
}

// Nlst returns a list of file in a directory, by default the current.
func (ftp *FTP) Nlst(params ...string) (filelist []string, err error) {
	return ftp.getList(NLST_FTP_CMD, params...)
}

// Dir returns a list of file in a directory in long form, by default the current.
func (ftp *FTP) Dir(params ...string) (filelist []string, err error) {
	return ftp.getList(LIST_FTP_CMD, params...)
}

func (ftp *FTP) getList(cmd FtpCmd, params ...string) (filelist []string, err error) {
	files := make([]string, 0, 50)
	sw := &stringSliceWriter{files}
	if err = ftp.GetLines(cmd, sw, params...); err != nil {
		return nil, err
	}
	return sw.s, nil
}

// Rename renames a file.
func (ftp *FTP) Rename(fromname string, toname string) (response *Response, err error) {
	tempResponse, err := ftp.SendAndRead(RENAMEFROM_FTP_CMD, fromname)
	if err != nil {
		return nil, err
	}
	if tempResponse.getFirstChar() != "3" {
		err = NewErrReply(errors.New(tempResponse.Message))
		return nil, err
	}
	return ftp.SendAndRead(RENAMETO_FTP_CMD, toname)
}

// Delete deletes a file.
func (ftp *FTP) Delete(filename string) (response *Response, err error) {
	tempResponse, err := ftp.SendAndRead(DELETE_FTP_CMD, filename)
	if err != nil {
		return nil, err
	}
	if c := tempResponse.Code; c == 250 || c == 200 {
		return tempResponse, nil
	} else {
		return nil, NewErrReply(errors.New(tempResponse.Message))
	}
	return
}

// Cwd changes to current directory.
func (ftp *FTP) Cwd(dirname string) (response *Response, err error) {
	if dirname == ".." {
		return ftp.SendAndRead(CDUP_FTP_CMD)
	} else if dirname == "" {
		dirname = "."
	}
	return ftp.SendAndRead(CWD_FTP_CMD, dirname)
}

// Size retrieves the size of a file.
func (ftp *FTP) Size(filename string) (size int, err error) {
	response, err := ftp.SendAndRead(SIZE_FTP_CMD, filename)
	if response.Code == 213 {
		size, _ = strconv.Atoi(strings.TrimSpace(response.Message[3:]))
		return size, err
	}
	return
}

// Mkd creates a directory and returns its full pathname.
func (ftp *FTP) Mkd(dirname string) (dname string, err error) {
	var response *Response
	response, err = ftp.SendAndRead(MKDIR_FTP_CMD, dirname)
	if err != nil {
		return
	}
	// fix around non-compliant implementations such as IIS shipped
	// with Windows server 2003
	if response.Code != 257 {
		return "", nil
	}
	return parse257(response)
}

// Rmd removes a directory.
func (ftp *FTP) Rmd(dirname string) (response *Response, err error) {
	return ftp.SendAndRead(RMDIR_FTP_CMD, dirname)
}

// Pwd returns the current working directory.
func (ftp *FTP) Pwd() (dirname string, err error) {
	response, err := ftp.SendAndRead(PWDIR_FTP_CMD)
	// fix around non-compliant implementations such as IIS shipped
	// with Windows server 2003
	if err != nil {
		return "", err
	}
	if response.Code != 257 {
		return "", nil
	}
	return parse257(response)
}

// Quits sends a QUIT command and closes the connection.
func (ftp *FTP) Quit() (response *Response, err error) {
	response, err = ftp.SendAndRead(QUIT_FTP_CMD)
	ftp.conn.Close()
	return
}

// DownloadFile downloads a file and stores it locally.
// There are two modes:
// - binary, 				useLineMode = false
// - line by line (text), 	useLineMode = true
func (ftp *FTP) DownloadFile(remotename string, localpath string, useLineMode bool) (err error) {
	// remove local file
	os.Remove(localpath)
	var f *os.File
	f, err = os.OpenFile(localpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	defer f.Close()

	if err != nil {
		return
	}

	if useLineMode {
		w := newTextFileWriter(f)
		defer w.bw.Flush() // remember to flush
		if err = ftp.GetLines(RETR_FTP_CMD, w, remotename); err != nil {
			return err
		}
	} else {
		if err = ftp.GetBytes(RETR_FTP_CMD, f, BLOCK_SIZE, remotename); err != nil {
			return err
		}
	}

	return err
}

// UploadFile uploads a file from a local path to the current folder (see Cwd too) on the FTP server.
// A remotename needs to be specified.
// There are two modes set via the useLineMode flag:
// - binary, 				useLineMode = false
// - line by line (text), 	useLineMode = true
func (ftp *FTP) UploadFile(remotename string, localpath string, useLineMode bool, callback Callback) (err error) {
	var f *os.File
	f, err = os.Open(localpath)
	defer f.Close()

	if err != nil {
		return
	}

	if useLineMode {
		if err = ftp.StoreLines(STORE_FTP_CMD, f, remotename, localpath, callback); err != nil {
			return err
		}
	} else {
		if err = ftp.StoreBytes(STORE_FTP_CMD, f, BLOCK_SIZE, remotename, localpath, callback); err != nil {
			return err
		}
	}

	return err
}

// Opts returns a list of file in a directory in long form, by default the current.
func (ftp *FTP) Opts(params ...string) (response *Response, err error) {
	return ftp.SendAndRead(OPTS_FTP_CMD, params...)
}

// GetLines retrieves data in line mode.
// Args:
//        cmd: A RETR, LIST, NLST, or MLSD command.
//        writer: of interface type io.Writer that is called for each line with the trailing CRLF stripped.
//
// returns:
//        The response code.
func (ftp *FTP) GetLines(cmd FtpCmd, writer io.Writer, params ...string) (err error) {
	var conn net.Conn
	if _, err = ftp.SendAndRead(TYPE_A_FTP_CMD); err != nil {
		return
	}

	// wrap this code up to guarantee the connection disposal via a defer
	separateCall := func() error {
		if conn, _, err = ftp.transferCmd(cmd, params...); err != nil {
			return err
		}
		defer conn.Close() // close the connection on exit

		ftpReader := textproto.NewConn(conn)
		ftp.writeInfo("Try and get lines via connection for remote address:", conn.RemoteAddr().String())

		for {
			line, err := ftpReader.ReadLineBytes()

			if err != nil {
				if err == io.EOF {
					ftp.writeInfo("Reached end of buffer with line:", line)
					break
				}
				return err
			}

			if _, err1 := writer.Write(line); err1 != nil {
				return err1
			}

		}
		return nil

	}

	if err := separateCall(); err != nil {
		return err
	}

	ftp.writeInfo("Reading final empty line")
	_, err = ftp.Read(cmd)
	return

}

// GetBytes retrieves data in binary mode.
// Args:
//        cmd: A RETR command.
//        callback: A single parameter callable to be called on each
//                  block of data read.
//        blocksize: The maximum number of bytes to read from the
//                  socket at one time.  [default: 8192]
//
//Returns:
//        The response code.
func (ftp *FTP) GetBytes(cmd FtpCmd, writer io.Writer, blocksize int, params ...string) (err error) {
	var conn net.Conn
	if _, err = ftp.SendAndRead(TYPE_I_FTP_CMD); err != nil {
		return
	}

	// wrap this code up to guarantee the connection disposal via a defer
	separateCall := func() error {
		if conn, _, err = ftp.transferCmd(cmd, params...); err != nil {
			return err
		}
		defer conn.Close() // close the connection on exit

		bufReader := bufio.NewReaderSize(conn, blocksize)

		ftp.writeInfo("Try and get bytes via connection for remote address:", conn.RemoteAddr().String())

		s := make([]byte, blocksize)
		var n int

		for {

			n, err = bufReader.Read(s)
			ftp.writeInfo("GETBYTES: Number of bytes read:", n)
			if _, err1 := writer.Write(s[:n]); err1 != nil {
				return err1
			}

			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

		}

		return nil
	}

	if err := separateCall(); err != nil {
		return err
	}

	_, err = ftp.Read(cmd)
	return
}

// StoreLines stores a file in line mode.
//
//      Args:
//        cmd: A STOR command.
//        reader: A reader object with a ReadLine() method.
//        callback: An optional single parameter callable that is called on
//                on each line after it is sent.  [default: None]
//
//      Returns:
//        The response code.
func (ftp *FTP) StoreLines(cmd FtpCmd, reader io.Reader, remotename string, filename string, callback Callback) (err error) {
	var conn net.Conn
	if _, err = ftp.SendAndRead(TYPE_A_FTP_CMD); err != nil {
		return
	}

	// wrap this code up to guarantee the connection disposal via a defer
	separateCall := func() error {
		if conn, _, err = ftp.transferCmd(cmd, remotename); err != nil {
			return err
		}
		defer conn.Close() // close the connection on exit

		ftp.writeInfo("Try and write lines via connection for remote address:", conn.RemoteAddr().String())

		//lineReader := bufio.NewReader(reader)
		lineReader := bufio.NewReader(reader)

		var tot int64

		for {
			var n int
			var eof bool
			line, _, err := lineReader.ReadLine()
			if err != nil {
				eof = err == io.EOF
				if !eof {
					return err
				}
			}

			// !Remember to convert to string (UTF-8 encoding)
			if !eof {
				n, err = fmt.Fprintln(conn, string(line))
				if err != nil {
					return err
				}
			}
			if callback != nil {
				tot += int64(n)
				callback(&CallbackInfo{remotename, filename, tot, eof})
			}

			if eof {
				break
			}
		}
		return nil

	}

	if err := separateCall(); err != nil {
		return err
	}

	ftp.writeInfo("Reading final empty line")

	_, err = ftp.Read(cmd)
	return

}

// StoreBytes uploads bytes in chunks defined by the blocksize parameter.
// It uses an io.Reader to read the input data.
func (ftp *FTP) StoreBytes(cmd FtpCmd, reader io.Reader, blocksize int, remotename string, filename string, callback Callback) (err error) {
	var conn net.Conn
	if _, err = ftp.SendAndRead(TYPE_I_FTP_CMD); err != nil {
		return
	}

	// wrap this code up to guarantee the connection disposal via a defer
	separateCall := func() error {
		if conn, _, err = ftp.transferCmd(cmd, remotename); err != nil {
			return err
		}
		defer conn.Close() // close the connection on exit

		bufReader := bufio.NewReaderSize(reader, blocksize)

		ftp.writeInfo("Try and store bytes via connection for remote address:", conn.RemoteAddr().String())

		s := make([]byte, blocksize)

		var tot int64

		for {
			var nr, nw int
			var eof bool

			nr, err = bufReader.Read(s)

			eof = err == io.EOF
			if err != nil && !eof {
				return err
			}

			if nw, err = conn.Write(s[:nr]); err != nil {
				return err
			}

			if callback != nil {
				tot += int64(nw)
				callback(&CallbackInfo{remotename, filename, tot, eof})
			}

			if eof {
				break
			}
		}
		return nil
	}

	if err := separateCall(); err != nil {
		return err
	}

	_, err = ftp.Read(cmd)
	return
}

// transferCmd initializes a tranfer over the data connection.
//
// If the transfer is active, send a port command and the tranfer command
// then accept the connection. If the server is passive, send a pasv command, connect to it
// and start the tranfer command. Either way return the connection and the expected size of the transfer.
// The expected size may be none if it could be not be determined.
func (ftp *FTP) transferCmd(cmd FtpCmd, params ...string) (conn net.Conn, size int, err error) {

	var listener net.Listener

	ftp.writeInfo("Server is passive:", ftp.passiveserver)
	if ftp.passiveserver {
		host, port, error := ftp.makePasv()
		if ftp.conn.LocalAddr().Network() != host {
			ftp.writeInfo("The remote server answered with a different host address, which is", host, ", using the orginal host instead:", ftp.Host)
			host = ftp.Host
		}
		if error != nil {
			return nil, -1, error
		}

		addr := fmt.Sprintf("%s:%d", host, port)
		if conn, err = ftp.dialer.Dial("tcp", addr); err != nil {
			ftp.writeInfo("Dial error, address:", addr, "error:", err, "proxy enabled:", ftp.dialer != proxy.Direct)
			return
		}

	} else {
		if listener, err = ftp.makePort(); err != nil {
			return
		}
		ftp.writeInfo("Listener created for non-passive mode")

	}

	var resp *Response
	if resp, err = ftp.SendAndRead(cmd, params...); err != nil {
		resp = nil
		return
	}

	// Some servers apparently send a 200 reply to
	// a LIST or STOR command, before the 150 reply
	// (and way before the 226 reply). This seems to
	// be in violation of the protocol (which only allows
	// 1xx or error messages for LIST), so we just discard
	// this response.
	if resp.getFirstChar() == "2" {
		resp, err = ftp.Read(cmd)
	}
	if resp.getFirstChar() != "1" {
		err = NewErrReply(errors.New(resp.Message))
		return
	}

	// not passive, open connection and close it then
	if listener != nil {
		ftp.writeInfo("Preparing to listen for non-passive mode.")
		if conn, err = listener.Accept(); err != nil {
			conn = nil
			return
		}
		ftp.writeInfo("Trying to communicate with local host: ", conn.LocalAddr())
		defer listener.Close() // close after getting the connection
	}

	if resp.Code == 150 {
		// this is conditional in case we received a 125
		ftp.writeInfo("Parsing return code 150")
		size, err = parse150ForSize(resp)
	}
	return conn, size, err
}

// makePort creates a new communication port and return a listener for this.
func (ftp *FTP) makePort() (listener net.Listener, err error) {

	tcpAddr := ftp.conn.LocalAddr()
	network := tcpAddr.Network()

	var la *net.TCPAddr
	if la, err = net.ResolveTCPAddr(network, tcpAddr.String()); err != nil {
		return
	}
	// get the new address
	newad := la.IP.String() + ":0" // any available port

	ftp.writeInfo("The new local address in makePort is:", newad)

	listening := runServer(newad, network)
	list := <-listening // wait for server to start and accept
	if list == nil {
		return nil, errors.New("Unable to create listener")
	}

	la, _ = net.ResolveTCPAddr(list.Addr().Network(), list.Addr().String())
	ftp.writeInfo("Trying to listen locally at: ", la.IP.String(), " on new port:", la.Port)

	_, err = ftp.SendPort(la.IP.String(), la.Port)

	return list, err
}

func runServer(laddr string, network string) chan net.Listener {
	listening := make(chan net.Listener)
	go func() {
		l, err := net.Listen(network, laddr)
		if err != nil {
			log.Fatalf("net.Listen(%q, %q) = _, %v", network, laddr, err)
			listening <- nil
			return
		}
		listening <- l
	}()
	return listening
}

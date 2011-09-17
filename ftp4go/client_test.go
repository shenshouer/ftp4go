package ftp4go

import (
	"testing"
	"os"
	"strings"
	"path/filepath"
	"fmt"
	"time"
)

func askParameter(question string, defaultValue string) (inputValue string, err os.Error) {
	fmt.Print(question)
	//originalStdout := os.Stdout
	//os.Stdout, _ = os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	//defer func(){os.Stdout = originalStdout}()
	const NBUF = 512
	var buf [NBUF]byte
	switch nr, er := os.Stdin.Read(buf[:]); true {
	case nr < 0:
		fmt.Print(os.Stderr, "Error reading parameter. Error: ", er)
		os.Exit(1)
	case nr == 0: //EOF
		inputValue, err = defaultValue, os.NewError("Invalid parameter")
	case nr > 0:
		inputValue, err = strings.TrimSpace(string(buf[0:nr])), nil
		if len(inputValue) == 0 {
			inputValue = defaultValue
		}
	}
	//fmt.Println("The input value is:", inputValue, " with length: ", len(inputValue))
	return inputValue, err
}

func startStats() (stats chan *CallbackInfo, fileUploaded chan bool, quit chan bool) {
	stats = make(chan *CallbackInfo, 100)
	quit = make(chan bool)
	fileUploaded = make(chan bool, 100)

	files := make(map[string][2]int64, 100)

	go func() {
		for {
			select {
			case st := <-stats:
				// do not wait here, the buffered request channel is the barrier

				go func() {
					pair, ok := files[st.Resourcename]
					var pos, size int64
					if !ok {
						fi, _ := os.Stat(st.Filename)

						files[st.Resourcename] = [2]int64{fi.Size, pos}
						size = fi.Size
					} else {
						pos = pair[1] // position correctly for writing
						size = pair[0]
					}

					mo := int((float32(st.BytesTransmitted)/float32(size))*100) / 10
					msg := fmt.Sprintf("File: %s - received: %d percent\n", st.Resourcename, mo*10)
					if st.Eof {
						fmt.Println("Uploaded (reached EOF) file:", st.Resourcename)
						fileUploaded <- true // done here
					} else {
						fmt.Print(msg)
					}
					/*
						if size <= st.BytesTransmitted {	
							fileUploaded <- true // done here
						}
					*/
				}()
			case <-quit:
				fmt.Println("Stopping workers")
				return // get out
			}
		}
	}()
	return
}

func NewFtpConn(logl int, t *testing.T) (ftpClient *FTP, err os.Error) {
	ftpAddress := "ftp.drivehq.com"
	ftpPort := 21
	username := "goftptest"
	password := "g0ftpt3st"

	ftpClient = NewFTP(logl) // 1 for debugging

	ftpClient.SetPassive(true)

	// connect
	_, err = ftpClient.Connect(ftpAddress, ftpPort)
	if err != nil {
		t.Fatalf("The FTP connection could not be established, error: ", err.String())
	}

	t.Logf("Connecting with username: %s and password %s", username, password)
	_, err = ftpClient.Login(username, password, "")
	if err != nil {
		t.Fatalf("The user could not be logged in, error: %s", err.String())
	}

	return

}

func TestFeatures(t *testing.T) {

	ftpClient, err := NewFtpConn(0, t)
	defer ftpClient.Quit()

	if err != nil {
		return
	}

	homefolder := "/PublicFolder"

	var resp *Response
	var cwd string
	resp, err = ftpClient.Cwd(homefolder) // home
	if err != nil {
		t.Fatalf("error: ", err.String(), ", response:", resp.Message)
	}

	cwd, err = ftpClient.Pwd()
	t.Log("The current folder is", cwd)

	t.Log("Testings Mlsd")
	ls, err := ftpClient.Mlsd(".", []string{"type", "size"})
	if err != nil {
		t.Logf("The ftp command MLSD does not work or is not supported, error: %s", err.String())
	} else {
		for _, l := range ls {
			t.Logf("\nMlsd entry: %s, facts: %v", l.Name, l.Facts)
		}
	}

	t.Logf("Testing upload\n")
	test_f := "test"
	maxSimultaneousConns := 1

	t.Log("Cleaning up before testing")
	var cleanup = func() os.Error { return cleanupFolderTree(ftpClient, test_f, homefolder, t) }
	cleanup()
	defer cleanup() // at the end again

	var n int

	n, err = ftpClient.UploadDirTree(test_f, homefolder, maxSimultaneousConns, nil, nil)
	if err != nil {
		t.Fatalf("Error uploading folder tree %s, error:\n", test_f, err)
	}
	t.Logf("Uploaded %d files.\n", n)

	t.Log("Checking download integrity by downloading the uploaded files and comparing the sizes")
	ftpClient.Cwd(homefolder)

	checkintegrity := func(fi string, istext bool) {
		t.Logf("Checking download integrity of file %s\n", fi)
		tkns := strings.Split(fi, "/")
		ficp := "ftptest_" + tkns[len(tkns)-1]
		err = ftpClient.DownloadFile(fi, ficp, istext)
		if err != nil {
			t.Fatalf("Error downloading file %s, error: %s", fi, err)
		}
		defer os.Remove(ficp)
		ofi, _ := os.Open(fi)
		defer ofi.Close()
		oficp, _ := os.Open(ficp)
		defer oficp.Close()

		s1, _ := ofi.Stat()
		s2, _ := oficp.Stat()

		if s1.Size != s2.Size {
			t.Errorf("The size of real file %s and the downloaded copy %s differ, size local: %d, size remote: %d", fi, ficp, s1.Size, s2.Size)
		}
	}

	fstochk := map[string]bool{"test/test.txt": true, "test/test.jpg": false}
	for s, v := range fstochk {
		checkintegrity(s, v)
	}

}

func TestRecursion(t *testing.T) {

	ftpClient, err := NewFtpConn(0, t)
	defer ftpClient.Quit()

	if err != nil {
		return
	}

	test_f := "test"
	noiterations := 2
	homefolder := "/PublicFolder"
	maxSimultaneousConns := 1

	t.Log("Cleaning up before testing")

	var cleanup = func() os.Error { return cleanupFolderTree(ftpClient, test_f, homefolder, t) }

	var check = func(f string) os.Error { return checkFolder(ftpClient, f, homefolder, t) }

	defer cleanup() // at the end again

	stats, fileUploaded, _ := startStats()
	var collector = func(info *CallbackInfo) {
		if info.Eof {
			stats <- info // pipe in for stats	
		}
	} // do not block the call

	var n int
	for i := 0; i < noiterations; i++ {
		t.Logf("\n -- Uploading folder tree: %s, iteration %d\n", filepath.Base(test_f), i+1)

		cleanup()
		t.Logf("Sleeping a second\n")
		time.Sleep(1e9)

		n, err = ftpClient.UploadDirTree(test_f, homefolder, maxSimultaneousConns, nil, collector)
		if err != nil {
			t.Fatalf("Error uploading folder tree %s, error:\n", test_f, err)
		}

		// wait for all stats to finish
		for k := 0; k < n; k++ {
			<-fileUploaded
		}

		t.Logf("Uploaded %d files.\n", n)

		check("test")
		check("test/subdir")
	}

}

// FTP routine utils


func checkFolder(ftpClient *FTP, f string, homefolder string, t *testing.T) (err os.Error) {

	_, err = ftpClient.Cwd(homefolder)
	if err != nil {
		t.Fatalf("Error in Cwd for folder %s:", homefolder, err.String())
	}

	defer ftpClient.Cwd(homefolder) //back to home at the end

	t.Logf("Checking subfolder %s", f)
	dirs := filepath.SplitList(f)
	for _, d := range dirs {
		_, err = ftpClient.Cwd(d)
		if err != nil {
			t.Fatalf("The folder %s was not created.", f)
		}
		ftpClient.Cwd("..")
	}

	var filelist []string
	if filelist, err = ftpClient.Nlst(); err != nil {
		t.Fatalf("No files in folder %s on the ftp server", f)
	}

	dir, _ := os.Open(f)
	files, _ := dir.Readdirnames(-1)
	fno := len(files)
	t.Logf("No of files in local folder %s is: %d", f, fno)

	for _, locF := range files {
		t.Logf("Checking local file or folder %s", locF)
		fi, err := os.Stat(locF)
		if err == nil && !fi.IsDirectory() {
			var found bool
			for _, remF := range filelist {
				if strings.Contains(strings.ToLower(remF), strings.ToLower(locF)) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("The local file %s could not be found at the server", locF)
			}
		}
	}

	return

}

func cleanupFolderTree(ftpClient *FTP, test_f string, homefolder string, t *testing.T) (err os.Error) {

	_, err = ftpClient.Cwd(homefolder)
	if err != nil {
		t.Fatalf("Error in Cwd for folder %s:", homefolder, err.String())
	}

	defer ftpClient.Cwd(homefolder) //back to home at the end

	t.Logf("Removing directory tree %s.", test_f)

	if err := ftpClient.RemoveRemoteDirTree(test_f); err != nil {
		if err != DIRECTORY_NON_EXISTENT {
			t.Fatalf("Error:", err.String())
		}
	}

	return
}

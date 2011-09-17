// Package ftp implements an FTP client.
package ftp4go

import (
	"os"
	"strings"
	"path/filepath"
	"sort"
	"fmt"
)

var DIRECTORY_NON_EXISTENT = os.NewError("The folder does not exist and can not be removed")

// RemoveRemoteDirTree removes a remote folder and all of its subfolders recursively.
// The current directory is then set to the orginal one before the operation or to the root of the deleted folder if it fails.
func (ftp *FTP) RemoveRemoteDirTree(remoteDir string) (err os.Error) {

	var pwd string
	if pwd, err = ftp.Pwd(); err != nil {
		return
	}

	// go back to original wd in a separate routine, if this fails stay where we are, onle level before the folder
	defer ftp.Cwd(pwd)

	return ftp.removeRemoteDirTree(remoteDir)
}

// removeRemoteDirTree removes a remote folder and all of its subfolders recursively.
// The error DIRECTORY_NON_EXISTENT of type os.Error is thrown if the FTP folder does not exist.
func (ftp *FTP) removeRemoteDirTree(remoteDir string) (err os.Error) {
	ftp.writeInfo("Changing working remote dir to:", remoteDir)

	if _, err = ftp.Cwd(remoteDir); err != nil {
		return DIRECTORY_NON_EXISTENT
	}

	ftp.writeInfo("Cleaning up remote folder:", remoteDir)

	var filelist []string
	if filelist, err = ftp.Dir(); err != nil {
		return err
	}

	for _, s := range filelist {
		subStrings := strings.Fields(s) // split on whitespace
		perm := subStrings[0]
		fname := subStrings[len(subStrings)-1] // file name, assume 'drw... ... filename'
		switch {
		case fname == "." || fname == "..":
			continue
		case perm[0] != 'd':
			// file, delete
			ftp.Delete(fname)
		case perm[0] == 'd': // directory 
			if err = ftp.RemoveRemoteDirTree(fname); err != nil {
				return err
			}
		}
	}
	ftp.Cwd("..")
	if _, err = ftp.Rmd(remoteDir); err != nil {
		return err
	}
	return nil

}

// UploadDirTree uploads a local directory and all of its subfolders
// localDir 		-> path to the local folder to upload along with all of its subfolders.
// remoteRootDir 	-> the root folder on the FTP server where to store the localDir tree.
// excludedDirs		-> a slice of folder names to exclude from the uploaded directory tree.
// callback			-> a callback function, which is called synchronously. Do remember to collect data in a go routine for instance if you do not want the upload to block.
// Returns the number of files uploaded and an error if any.
//
// The current workding directory is set back to the initial value at the end.
func (ftp *FTP) UploadDirTree(localDir string, remoteRootDir string, maxSimultaneousConns int, excludedDirs []string, callback Callback) (n int, err os.Error) {
	//print("Uploading tree:", localDir, "\n")

	if len(remoteRootDir) == 0 {
		return n, os.NewError("A valid remote root folder with write permission needs specifying.")
	}

	var pwd string
	if pwd, err = ftp.Pwd(); err != nil {
		return
	}

	if _, err = ftp.Cwd(remoteRootDir); err != nil {
		return n, nil
	}
	//go back to original wd
	defer ftp.Cwd(pwd)

	var (
		reqs chan *request
		//resps map[*request]chan os.Error
		quit chan bool
	)

	// NOTE: stick this to 1, it does not work otherwise
	//maxSimultaneousConns = 1
	useGoRoutines := false //maxSimultaneousConns >= 1
	if useGoRoutines {
		reqs, quit = startServer(ftp, maxSimultaneousConns, callback)
	}

	//all lower case
	var exDirs sort.StringSlice
	if len(excludedDirs) > 0 {
		exDirs = sort.StringSlice(excludedDirs)
		for _, v := range exDirs {
			v = strings.ToLower(v)
		}
		exDirs.Sort()
	}

	err = ftp.uploadDirTree(localDir, exDirs, reqs, useGoRoutines, quit, callback, &n)
	if err != nil {
		ftp.writeInfo(fmt.Sprintf("An error while uploading the folder %s occurred.", localDir))
	}

	if useGoRoutines {
		// collect responses
		quit <- true // stopping the server
	}

	return n, err
}

func (ftp *FTP) uploadDirTree(localDir string, excludedDirs sort.StringSlice, queue chan *request, useGoRoutines bool, quit chan bool, callback Callback, n *int) (err os.Error) {

	_, dir := filepath.Split(localDir)
	ftp.writeInfo("The directory where to upload is:", dir)
	if _, err = ftp.Mkd(dir); err != nil {
		return
	}

	_, err = ftp.Cwd(dir)
	if err != nil {
		ftp.writeInfo(fmt.Sprintf("An error occurred while CWD, err: %s.", err))
		return
	}
	defer ftp.Cwd("..")
	globSearch := filepath.Join(localDir, "*")
	ftp.writeInfo("Looking up files in", globSearch)
	var files []string
	files, err = filepath.Glob(globSearch) // find all files in folder
	if err != nil {
		return
	}
	ftp.writeInfo("Found", len(files), "files")
	sort.Strings(files) // sort by name

	dirRequests := []*request{}
	for _, s := range files {
		_, fname := filepath.Split(s) // find file name
		localPath := filepath.Join(localDir, fname)
		ftp.writeInfo("Uploading file or dir:", localPath)
		var f *os.FileInfo
		if f, err = os.Stat(localPath); err != nil {
			return
		}
		if !f.IsDirectory() {
			if useGoRoutines {
				// response channel with one shot, let the response go through without blocking
				r := &request{fname, localPath, false, make(chan os.Error, 1)}
				dirRequests = append(dirRequests, r) // add request to slice
			} else {
				err = ftp.UploadFile(fname, localPath, false, callback) // always binary upload
				if err != nil {
					return
				}
				*n += 1 // increment
			}
		} else {
			if len(excludedDirs) > 0 {
				ftp.writeInfo("Checking folder:", fname)
				lfname := strings.ToLower(fname)
				idx := sort.SearchStrings(excludedDirs, lfname)
				if idx < len(excludedDirs) && excludedDirs[idx] == lfname {
					ftp.writeInfo("Excluding folder:", s)
					continue
				}
			}
			if err = ftp.uploadDirTree(localPath, excludedDirs, queue, useGoRoutines, quit, callback, n); err != nil {
				return
			}
		}

	}

	// upload simulaneously BUT ON A FOLDER BASIS!
	if useGoRoutines {
		for _, r := range dirRequests {
			queue <- r
		}

		// collect responses for the current folder
		for _, r := range dirRequests {
			if e := <-r.resultChan; e == nil {
				ftp.writeInfo(fmt.Sprintf("Success, file: %s", r.localpath))
			} else {
				ftp.writeInfo(fmt.Sprintf("Error, file: %s. Error %s", r.localpath, e))
				err = e // save last error
				// quit <- true // get out and shut down
				// break
			}
		}
	}

	return
}

type request struct {
	remotename string
	localpath  string
	istext     bool
	resultChan chan os.Error
}

func startServer(ftp *FTP, maxUploads int, callback Callback) (queue chan *request, quit chan bool) {
	queue = make(chan *request)
	quit = make(chan bool)
	sem := make(chan int, maxUploads)

	go func() {
		for {
			select {
			case req := <-queue:
				go func() {
					sem <- 1 // Wait for active queue to drain.
					ftp.writeInfo("THE QUEUE HAS A SLOT: uploading file:", req.localpath)
					e := ftp.UploadFile(req.remotename, req.localpath, req.istext, callback)
					req.resultChan <- e
					<-sem // Done; enable next request to run.
				}()
			case <-quit:
				ftp.writeInfo("Stopping workers")
				return // get out
			}

		}
	}()
	return
}

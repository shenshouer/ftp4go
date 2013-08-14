// Package ftp implements an FTP client.
package ftp4go

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var DIRECTORY_NON_EXISTENT = errors.New("The folder does not exist and can not be removed")

// RemoveRemoteDirTree removes a remote folder and all of its subfolders recursively.
// The current directory is then set to the orginal one before the operation or to the root of the deleted folder if it fails.
func (ftp *FTP) RemoveRemoteDirTree(remoteDir string) (err error) {

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
func (ftp *FTP) removeRemoteDirTree(remoteDir string) (err error) {
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
func (ftp *FTP) UploadDirTree(localDir string, remoteRootDir string, maxSimultaneousConns int, excludedDirs []string, callback Callback) (n int, err error) {

	if len(remoteRootDir) == 0 {
		return n, errors.New("A valid remote root folder with write permission needs specifying.")
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

	//all lower case
	var exDirs sort.StringSlice
	if len(excludedDirs) > 0 {
		exDirs = sort.StringSlice(excludedDirs)
		for _, v := range exDirs {
			v = strings.ToLower(v)
		}
		exDirs.Sort()
	}

	err = ftp.uploadDirTree(localDir, exDirs, callback, &n)
	if err != nil {
		ftp.writeInfo(fmt.Sprintf("An error while uploading the folder %s occurred.", localDir))
	}

	return n, err
}

func (ftp *FTP) uploadDirTree(localDir string, excludedDirs sort.StringSlice, callback Callback, n *int) (err error) {

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

	for _, s := range files {
		_, fname := filepath.Split(s) // find file name
		localPath := filepath.Join(localDir, fname)
		ftp.writeInfo("Uploading file or dir:", localPath)
		var f os.FileInfo
		if f, err = os.Stat(localPath); err != nil {
			return
		}
		if !f.IsDir() {
			err = ftp.UploadFile(fname, localPath, false, callback) // always binary upload
			if err != nil {
				return
			}
			*n += 1 // increment
		} else {
			if len(excludedDirs) > 0 {
				ftp.writeInfo("Checking folder name:", fname)
				lfname := strings.ToLower(fname)
				idx := sort.SearchStrings(excludedDirs, lfname)
				if idx < len(excludedDirs) && excludedDirs[idx] == lfname {
					ftp.writeInfo("Excluding folder:", s)
					continue
				}
			}
			if err = ftp.uploadDirTree(localPath, excludedDirs, callback, n); err != nil {
				return
			}
		}

	}

	return
}

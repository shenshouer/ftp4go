This is an FTP client started as a port of the standard Python FTP client library   
Forked form code.google.com/p/ftp4go and change some dependenes of the package

# Installation

<code>go get github.com/shenshouer/ftp4go</code>

# How to use it
Import the library in your code and call the methods exposed by the FTP structure, for instance:
<pre>
package main  
import (
  "fmt"
  "os"
  ftp4go "github.com/shenshouer/ftp4go"
)  
func main() {
  ftpClient := ftp4go.NewFTP(0) // 1 for debugging
  //connect
  _, err := ftpClient.Connect("myFtpAddress", ftp4go.DefaultFtpPort)
  if err != nil {
    fmt.Println("The connection failed")
    os.Exit(1)
  }   
  defer ftpClient.Quit()
  _, err = ftpClient.Login("myUsername", "myPassword", "")
  if err != nil {
    fmt.Println("The login failed")
    os.Exit(1)
  }      
  //Print the current working directory
  var cwd string
  cwd, err = ftpClient.Pwd()
  if err != nil {
    fmt.Println("The Pwd command failed")
    os.Exit(1)
  }
  fmt.Println("The current folder is", cwd)
}
</pre>

# More on the code
Being a port of a Python library, the original Python version is probably the best reference.  
<a href="http://docs.python.org/dev/library/ftplib.html">Python ftplib</a>

## Differences to the original version
Some new methods have been implemented to upload and download files, recursively in a folder as well.

## TODOs and unsupported functionality
* TLS is not supported yet
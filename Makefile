include $(GOROOT)/src/Make.inc

TARG=ftp4go
GOFMT=gofmt -s -spaces=true -tabindent=false -tabwidth=4

GOFILES=\
  client.go\
  clientproto.go\
  clientutil.go\

include $(GOROOT)/src/Make.pkg

format:
	${GOFMT} -w ${GOFILES}


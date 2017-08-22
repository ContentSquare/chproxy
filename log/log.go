package log

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"io"
)

var (
	stdLogFlags     = log.LstdFlags | log.Lshortfile | log.LUTC
	outputCallDepth = 2

	debugLogger = newDebugLogger(os.Stderr, "DEBUG: ", stdLogFlags)
	infoLogger  = log.New(os.Stderr, "INFO: ", stdLogFlags)
	errorLogger = log.New(os.Stderr, "ERROR: ", stdLogFlags)
	fatalLogger = log.New(os.Stderr, "FATAL: ", log.LstdFlags|log.Llongfile|log.LUTC)

)


// Suppresses all output from logs if `suppress` is true
// used while testing
func SuppressOutput(suppress bool) {
	if suppress {
		debugLogger.SetOutput(ioutil.Discard)
		infoLogger.SetOutput(ioutil.Discard)
		errorLogger.SetOutput(ioutil.Discard)
	} else {
		debugLogger.SetOutput(os.Stderr)
		infoLogger.SetOutput(os.Stderr)
		errorLogger.SetOutput(os.Stderr)
	}
}

func SetDebug(debug bool) {
	debugLogger.set(debug)
}

func Debugf(format string, args ...interface{}) {
	if !debugLogger.enabled() {
		return
	}

	s := fmt.Sprintf(format, args...)
	debugLogger.Output(outputCallDepth, s)
}

func Infof(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	infoLogger.Output(outputCallDepth, s)
}

func Errorf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	errorLogger.Output(outputCallDepth, s)
}

func Fatalf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	fatalLogger.Output(outputCallDepth, s)
	os.Exit(1)
}


type debugLog struct {
	*log.Logger

	sync.Mutex
	debug bool
}

func (dl *debugLog) set(debug bool) {
	dl.Lock()
	dl.debug = debug
	dl.Unlock()
}

func (dl *debugLog) enabled() bool {
	dl.Lock()
	defer dl.Unlock()
	return dl.debug
}

func newDebugLogger(out io.Writer, prefix string, flag int) *debugLogger {
	return &debugLog {
		Logger: log.New(out, prefix, flag),
	}
}
package log

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync/atomic"
)

var (
	stdLogFlags      = log.LstdFlags | log.LUTC
	stdDebugLogFlags = log.LstdFlags | log.Lshortfile | log.LUTC
	outputCallDepth  = 2

	DebugLogger = log.New(os.Stderr, "DEBUG: ", stdDebugLogFlags)
	InfoLogger  = log.New(os.Stderr, "INFO: ", stdLogFlags)
	ErrorLogger = log.New(os.Stderr, "ERROR: ", stdLogFlags)
	FatalLogger = log.New(os.Stderr, "FATAL: ", log.LstdFlags|log.Llongfile|log.LUTC)
)

// SuppressOutput suppresses all output from logs if `suppress` is true
// used while testing
func SuppressOutput(suppress bool) {
	if suppress {
		DebugLogger.SetOutput(ioutil.Discard)
		InfoLogger.SetOutput(ioutil.Discard)
		ErrorLogger.SetOutput(ioutil.Discard)
	} else {
		DebugLogger.SetOutput(os.Stderr)
		InfoLogger.SetOutput(os.Stderr)
		ErrorLogger.SetOutput(os.Stderr)
	}
}

var debug uint32

// SetDebug sets output into debug mode if true passed
func SetDebug(val bool) {
	if val {
		atomic.StoreUint32(&debug, 1)
		InfoLogger.SetFlags(stdDebugLogFlags)
		ErrorLogger.SetFlags(stdDebugLogFlags)
	} else {
		atomic.StoreUint32(&debug, 0)
		InfoLogger.SetFlags(stdLogFlags)
		ErrorLogger.SetFlags(stdLogFlags)
	}
}

// Debugf prints debug messages
func Debugf(format string, args ...interface{}) {
	if atomic.LoadUint32(&debug) == 0 {
		return
	}

	s := fmt.Sprintf(format, args...)
	DebugLogger.Output(outputCallDepth, s)
}

// Infof prints info messages
func Infof(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	InfoLogger.Output(outputCallDepth, s)
}

// Errorf prints warning messages
func Errorf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	ErrorLogger.Output(outputCallDepth, s)
}

// Fatalf prints fatal message and exits program
func Fatalf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	FatalLogger.Output(outputCallDepth, s)
	os.Exit(1)
}

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

	debugLogger = log.New(os.Stderr, "DEBUG: ", stdDebugLogFlags)
	infoLogger  = log.New(os.Stderr, "INFO: ", stdLogFlags)
	errorLogger = log.New(os.Stderr, "ERROR: ", stdLogFlags)
	fatalLogger = log.New(os.Stderr, "FATAL: ", log.LstdFlags|log.Llongfile|log.LUTC)

	// ErrorLogger is used outside the package.
	ErrorLogger = errorLogger

	// NilLogger suppresses all the log messages.
	NilLogger = log.New(ioutil.Discard, "", stdLogFlags)
)

// SuppressOutput suppresses all output from logs if `suppress` is true
// used while testing
func SuppressOutput(suppress bool) {
	if suppress {
		atomic.StoreUint32(&forbidDebug, 1)
		debugLogger.SetOutput(ioutil.Discard)
		infoLogger.SetOutput(ioutil.Discard)
		errorLogger.SetOutput(ioutil.Discard)
	} else {
		atomic.StoreUint32(&forbidDebug, 0)
		debugLogger.SetOutput(os.Stderr)
		infoLogger.SetOutput(os.Stderr)
		errorLogger.SetOutput(os.Stderr)
	}
}

var (
	debug uint32

	// hack to avoid race conditions in log package
	// see https://github.com/golang/go/issues/21935
	forbidDebug uint32
)

// SetDebug sets output into debug mode if true passed
func SetDebug(val bool) {
	if atomic.LoadUint32(&forbidDebug) == 1 {
		return
	}
	if val {
		atomic.StoreUint32(&debug, 1)
		infoLogger.SetFlags(stdDebugLogFlags)
		errorLogger.SetFlags(stdDebugLogFlags)
	} else {
		atomic.StoreUint32(&debug, 0)
		infoLogger.SetFlags(stdLogFlags)
		errorLogger.SetFlags(stdLogFlags)
	}
}

// Debugf prints debug message according to a format
func Debugf(format string, args ...interface{}) {
	if atomic.LoadUint32(&debug) == 0 {
		return
	}
	s := fmt.Sprintf(format, args...)
	debugLogger.Output(outputCallDepth, s)
}

// Debug prints debug message
func Debug(s string) {
	if atomic.LoadUint32(&debug) == 0 {
		return
	}
	debugLogger.Output(outputCallDepth, s)
}

// Infof prints info message according to a format
func Infof(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	Info(s)
}

// Info prints info message
func Info(s string) {
	infoLogger.Output(outputCallDepth, s)
}

// Errorf prints warning message according to a format
func Errorf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	Error(s)
}

// Error prints warning message
func Error(s string) {
	errorLogger.Output(outputCallDepth, s)
}

// Fatalf prints fatal message according to a format and exits program
func Fatalf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	Fatal(s)
}

// Fatal prints fatal message and exits program
func Fatal(s string) {
	fatalLogger.Output(outputCallDepth+1, s)
	os.Exit(1)
}

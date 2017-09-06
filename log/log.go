package log

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
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

// Suppresses all output from logs if `suppress` is true
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

var (
	mu    sync.Mutex
	debug bool
)

func SetDebug(val bool) {
	if val {
		InfoLogger.SetFlags(stdDebugLogFlags)
		ErrorLogger.SetFlags(stdDebugLogFlags)
	} else {
		InfoLogger.SetFlags(stdLogFlags)
		ErrorLogger.SetFlags(stdLogFlags)
	}

	mu.Lock()
	debug = val
	mu.Unlock()
}

func Debugf(format string, args ...interface{}) {
	mu.Lock()
	if !debug {
		mu.Unlock()
		return
	}
	mu.Unlock()

	s := fmt.Sprintf(format, args...)
	DebugLogger.Output(outputCallDepth, s)
}

func Infof(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	InfoLogger.Output(outputCallDepth, s)
}

func Errorf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	ErrorLogger.Output(outputCallDepth, s)
}

func Fatalf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	FatalLogger.Output(outputCallDepth, s)
	os.Exit(1)
}

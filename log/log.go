package log

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

var (
	stdLogFlags     = log.LstdFlags | log.Lshortfile | log.LUTC
	outputCallDepth = 2

	DebugLogger = log.New(os.Stderr, "DEBUG: ", stdLogFlags)
	InfoLogger  = log.New(os.Stderr, "INFO: ", stdLogFlags)
	ErrorLogger = log.New(os.Stderr, "ERROR: ", stdLogFlags)
	FatalLogger = log.New(os.Stderr, "FATAL: ", log.LstdFlags|log.Llongfile|log.LUTC)

	debug = flag.Bool("debug", false, "Whether print debug messages")
)

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

func Debugf(format string, args ...interface{}) {
	if !*debug {
		return
	}

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

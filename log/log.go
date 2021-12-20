package log

import (
	"fmt"
	"gopkg.in/natefinch/lumberjack.v2"
	"io/ioutil"
	"log"
	"os"
	"sync/atomic"
)

var (
	stdLogFlags     = log.LstdFlags | log.Lshortfile | log.LUTC
	outputCallDepth = 2

	debugLogger = log.New(os.Stderr, "DEBUG: ", stdLogFlags)
	infoLogger  = log.New(os.Stderr, "INFO: ", stdLogFlags)
	errorLogger = log.New(os.Stderr, "ERROR: ", stdLogFlags)
	fatalLogger = log.New(os.Stderr, "FATAL: ", stdLogFlags)

	// NilLogger suppresses all the log messages.
	NilLogger = log.New(ioutil.Discard, "", stdLogFlags)
)

func InitLogger(filename string,maxSize,maxBackups,maxAge int ,compress bool)  {
	if filename == ""{
		return
	}
	logger := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize, // megabytes for MB
		MaxBackups: maxBackups,
		MaxAge:     maxAge,    // days
		Compress:   compress, // disabled by default
	}

	debugLogger = log.New(logger, "DEBUG: ", stdLogFlags)
	infoLogger  = log.New(logger, "INFO: ", stdLogFlags)
	errorLogger = log.New(logger, "ERROR: ", stdLogFlags)
	fatalLogger = log.New(logger, "FATAL: ", stdLogFlags)
}

// SuppressOutput suppresses all output from logs if `suppress` is true
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

var debug uint32

// SetDebug sets output into debug mode if true passed
func SetDebug(val bool) {
	if val {
		atomic.StoreUint32(&debug, 1)
	} else {
		atomic.StoreUint32(&debug, 0)
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

// Infof prints info message according to a format
func Infof(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	infoLogger.Output(outputCallDepth, s)
}

// Errorf prints warning message according to a format
func Errorf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	errorLogger.Output(outputCallDepth, s)
}

// ErrorWithCallDepth prints err into error log using the given callDepth.
func ErrorWithCallDepth(err error, callDepth int) {
	s := err.Error()
	errorLogger.Output(outputCallDepth+callDepth, s)
}

// Fatalf prints fatal message according to a format and exits program
func Fatalf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	fatalLogger.Output(outputCallDepth, s)
	os.Exit(1)
}

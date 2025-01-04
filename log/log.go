package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
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
	NilLogger = log.New(io.Discard, "", stdLogFlags)
	replacer  *strings.Replacer
)

// SuppressOutput suppresses all output from logs if `suppress` is true
// used while testing
func SuppressOutput(suppress bool) {
	if suppress {
		debugLogger.SetOutput(io.Discard)
		infoLogger.SetOutput(io.Discard)
		errorLogger.SetOutput(io.Discard)
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
	debugLogger.Output(outputCallDepth, mask(s)) // nolint
}

// Infof prints info message according to a format
func Infof(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	infoLogger.Output(outputCallDepth, mask(s)) // nolint
}

// Errorf prints warning message according to a format
func Errorf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	errorLogger.Output(outputCallDepth, mask(s)) // nolint
}

// ErrorWithCallDepth prints err into error log using the given callDepth.
func ErrorWithCallDepth(err error, callDepth int) {
	s := err.Error()
	errorLogger.Output(outputCallDepth+callDepth, mask(s)) //nolint
}

// Fatalf prints fatal message according to a format and exits program
func Fatalf(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	fatalLogger.Output(outputCallDepth, mask(s)) // nolint
	os.Exit(1)
}

func mask(s string) string {
	if replacer == nil {
		return s
	}
	return replacer.Replace(s)
}

func InitReplacer(secrets []string) {
	//nolint:mnd // twice the size
	oldnew := make([]string, 0, len(secrets)*2)
	for _, s := range secrets {
		if s == "" {
			continue
		}
		oldnew = append(oldnew, s, "<xxx>")
	}
	replacer = strings.NewReplacer(oldnew...)
}

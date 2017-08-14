package log

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
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

func init() {
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
		s := <-c
		Infof("Obtained signal %q. Terminating...", s)
		time.Sleep(time.Second)
		os.Exit(0)
	}()
}

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
}

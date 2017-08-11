package log

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"flag"
)

var (
	stdLogFlags     = log.LstdFlags | log.Lshortfile | log.LUTC
	outputCallDepth = 2

	debugLogger  = log.New(os.Stderr, "DEBUG: ", stdLogFlags)
	infoLogger  = log.New(os.Stderr, "INFO: ", stdLogFlags)
	errorLogger = log.New(os.Stderr, "ERROR: ", stdLogFlags)
	fatalLogger = log.New(os.Stderr, "FATAL: ", log.LstdFlags|log.Llongfile|log.LUTC)

	debug = flag.Bool("debug", true, "Wheter print debug messages")
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

func Debugf(format string, args ...interface{}) {
	if !*debug {
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
}

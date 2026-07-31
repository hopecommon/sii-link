package log

import (
	"encoding/hex"
	"io"
	"log"
	"os"
	"sync"
)

var (
	debug    bool
	output   io.Writer = os.Stdout
	outputMu sync.RWMutex
)

func Init() {
	setOutput(os.Stdout)
}

func ConfigureFile(path string, maxBytes int64, backups int) (io.Closer, error) {
	writer, err := newRotatingWriter(path, maxBytes, backups)
	if err != nil {
		return nil, err
	}
	setOutput(writer)
	return writer, nil
}

func setOutput(writer io.Writer) {
	outputMu.Lock()
	output = writer
	log.SetOutput(writer)
	outputMu.Unlock()
}

func currentOutput() io.Writer {
	outputMu.RLock()
	defer outputMu.RUnlock()
	return output
}

func EnableDebug() {
	debug = true
}

func DisableDebug() {
	debug = false
}

func Print(v ...any) {
	log.Print(v...)
}

func DebugPrint(v ...any) {
	if debug {
		log.Print(v...)
	}
}

func Println(v ...any) {
	log.Println(v...)
}

func DebugPrintln(v ...any) {
	if debug {
		log.Println(v...)
	}
}

func Printf(format string, v ...any) {
	log.Printf(format, v...)
}

func DebugPrintf(format string, v ...any) {
	if debug {
		log.Printf(format, v...)
	}
}

func Fatal(v ...any) {
	log.Fatal(v...)
}

func Fatalf(format string, v ...any) {
	log.Fatalf(format, v...)
}

func DumpHex(buf []byte) {
	stdoutDumper := hex.Dumper(currentOutput())
	defer func(stdoutDumper io.WriteCloser) {
		_ = stdoutDumper.Close()
	}(stdoutDumper)
	_, _ = stdoutDumper.Write(buf)
}

func DebugDumpHex(buf []byte) {
	if debug {
		stdoutDumper := hex.Dumper(currentOutput())
		defer func(stdoutDumper io.WriteCloser) {
			_ = stdoutDumper.Close()
		}(stdoutDumper)
		_, _ = stdoutDumper.Write(buf)
	}
}

func NewLogger(prefix string) *log.Logger {
	return log.New(currentOutput(), prefix, log.LstdFlags)
}

package ilog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type message struct {
	content string
	level   Level
}

// Logger is the core struct of ilog package. It packs message and sends it to the writer.
type Logger struct {
	callDepth   int
	lowestLevel Level
	isRunning   int32
	wg          *sync.WaitGroup

	writers   []LogWriter
	msg       chan *message
	flush     chan *sync.WaitGroup
	syncWrite bool

	bufPool *BufPool

	quitCh chan struct{}
}

// New returns a default Logger instance.
func New() *Logger {
	return &Logger{
		callDepth:   1,
		lowestLevel: LevelFatal,
		wg:          new(sync.WaitGroup),
		msg:         make(chan *message, 4096),
		flush:       make(chan *sync.WaitGroup, 1),
		bufPool:     NewBufPool(),
		quitCh:      make(chan struct{}, 1),
		syncWrite:   true,
	}
}

// NewConsoleLogger returns a Logger instance with a console writer.
func NewConsoleLogger() *Logger {
	logger := New()
	consoleWriter := NewConsoleWriter()
	logger.AddWriter(consoleWriter)
	return logger
}

// Start starts the logger.
func (logger *Logger) Start() {
	if !atomic.CompareAndSwapInt32(&logger.isRunning, 0, 1) {
		return
	}
	if len(logger.writers) == 0 {
		fmt.Println("logger's writers is empty.")
		return
	}

	logger.wg.Add(1)
	go func() {
		defer func() {
			atomic.StoreInt32(&logger.isRunning, 0)
			logger.cleanMsg()
			for _, writer := range logger.writers {
				writer.Flush()
				writer.Close()
			}
			logger.wg.Done()
		}()

		for {
			select {
			case <-logger.quitCh:
				return
			case msg := <-logger.msg:
				logger.write(msg)
			case wg := <-logger.flush:
				logger.cleanMsg()
				logger.flushWriters()
				wg.Done()
			}
		}
	}()
}

// Stop stops the logger.
func (logger *Logger) Stop() {
	if !atomic.CompareAndSwapInt32(&logger.isRunning, 1, 0) {
		return
	}
	logger.quitCh <- struct{}{}
	logger.wg.Wait()
}

// SetCallDepth sets the logger's call depth.
func (logger *Logger) SetCallDepth(d int) {
	logger.callDepth = d
}

// AddWriter adds a writer to logger.
func (logger *Logger) AddWriter(writer LogWriter) error {
	if err := writer.Init(); err != nil {
		return err
	}
	logger.writers = append(logger.writers, writer)
	if logger.lowestLevel > writer.GetLevel() {
		logger.lowestLevel = writer.GetLevel()
	}
	return nil
}

// Flush flushes the logger.
func (logger *Logger) Flush() {
	if atomic.LoadInt32(&logger.isRunning) == 0 {
		return
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	select {
	case logger.flush <- wg:
		wg.Wait()
	default:
	}
}

// AsyncWrite sets logger's syncWrite to false.
func (logger *Logger) AsyncWrite() {
	logger.syncWrite = false
}

func (logger *Logger) write(msg *message) {
	wg := &sync.WaitGroup{}
	for _, writer := range logger.writers {
		if msg.level < writer.GetLevel() {
			continue
		}
		wg.Add(1)
		go func(lw LogWriter) {
			lw.Write(msg.content, msg.level)
			wg.Done()
		}(writer)
	}
	wg.Wait()

}

func (logger *Logger) flushWriters() {
	wg := &sync.WaitGroup{}
	for _, writer := range logger.writers {
		wg.Add(1)
		go func(lw LogWriter) {
			lw.Flush()
			wg.Done()
		}(writer)
	}
	wg.Wait()
}

// Debugf generates a debug-level log.
func (logger *Logger) Debugf(format string, v ...interface{}) {
	logger.genMsg(LevelDebug, fmt.Sprintf(format, v...))
}

// Infof generates a info-level log.
func (logger *Logger) Infof(format string, v ...interface{}) {
	logger.genMsg(LevelInfo, fmt.Sprintf(format, v...))
}

// Warnf generates a warn-level log.
func (logger *Logger) Warnf(format string, v ...interface{}) {
	logger.genMsg(LevelWarn, fmt.Sprintf(format, v...))
}

// Errorf generates a error-level log.
func (logger *Logger) Errorf(format string, v ...interface{}) {
	logger.genMsg(LevelError, fmt.Sprintf(format, v...))
}

// Fatalf generates a fatal-level log and exits the program.
func (logger *Logger) Fatalf(format string, v ...interface{}) {
	logger.genMsg(LevelFatal, fmt.Sprintf(format, v...))
	logger.Stop()
	os.Exit(1)
}

// Debug generates a debug-level log.
func (logger *Logger) Debug(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	logger.genMsg(LevelDebug, msg[:len(msg)-1])
}

// Info generates a info-level log.
func (logger *Logger) Info(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	logger.genMsg(LevelInfo, msg[:len(msg)-1])
}

// Warn generates a warn-level log.
func (logger *Logger) Warn(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	logger.genMsg(LevelWarn, msg[:len(msg)-1])
}

// Error generates a error-level log.
func (logger *Logger) Error(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	logger.genMsg(LevelError, msg[:len(msg)-1])
}

// Fatal generates a fatal-level log and exits the program.
func (logger *Logger) Fatal(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	logger.genMsg(LevelFatal, msg[:len(msg)-1])
	logger.Stop()
	os.Exit(1)
}

func (logger *Logger) genMsg(level Level, log string) {
	if level < logger.lowestLevel {
		return
	}
	if atomic.LoadInt32(&logger.isRunning) == 0 {
		return
	}
	buf := logger.bufPool.Get()
	defer logger.bufPool.Release(buf)

	buf.Write(levelBytes[level])
	buf.WriteString(" ")
	buf.WriteString(time.Now().Format("2006-01-02 15:04:05.000"))
	buf.WriteString(" ")
	buf.WriteString(location(logger.callDepth + 3))
	buf.WriteString(" ")
	buf.WriteString(log)
	buf.WriteString("\n")

	select {
	case logger.msg <- &message{buf.String(), level}:
		// default:
	}

	if logger.syncWrite {
		logger.Flush()
	}
}

func (logger *Logger) cleanMsg() {
	for {
		select {
		case msg := <-logger.msg:
			logger.write(msg)
		default:
			return
		}
	}
}

func location(deep int) string {
	_, file, line, ok := runtime.Caller(deep)
	if !ok {
		file = "???"
		line = 0
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}

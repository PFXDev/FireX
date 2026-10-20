package store

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm/logger"
)

const slowSQL = 200 * time.Millisecond

// sqlLogger matches gorm's default logger — errors and slow statements always,
// every statement in debug — except that debug can be switched on an open
// database. gorm reads its logger off a shared config with no locking, so the
// level has to live inside the logger rather than in a logger swapped in.
//
// It formats the lines itself rather than wrapping gorm's: gorm reports the
// first caller outside its own source, which would be this file every time.
type sqlLogger struct {
	verbose atomic.Bool
	out     *log.Logger
}

func (l *sqlLogger) LogMode(logger.LogLevel) logger.Interface { return l }

func (l *sqlLogger) Info(_ context.Context, msg string, data ...any) {
	if l.verbose.Load() {
		l.out.Printf("%s [info] "+msg, append([]any{caller()}, data...)...)
	}
}

func (l *sqlLogger) Warn(_ context.Context, msg string, data ...any) {
	l.out.Printf("%s [warn] "+msg, append([]any{caller()}, data...)...)
}

func (l *sqlLogger) Error(_ context.Context, msg string, data ...any) {
	l.out.Printf("%s [error] "+msg, append([]any{caller()}, data...)...)
}

func (l *sqlLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	var label string
	switch {
	case err != nil:
		label = " " + err.Error()
	case elapsed >= slowSQL:
		label = fmt.Sprintf(" SLOW SQL >= %v", slowSQL)
	case !l.verbose.Load():
		return
	}
	sql, rows := fc()
	affected := fmt.Sprint(rows)
	if rows == -1 {
		affected = "-"
	}
	l.out.Printf("%s%s\n[%.3fms] [rows:%s] %s", caller(), label, float64(elapsed.Nanoseconds())/1e6, affected, sql)
}

var loggerFile = func() string {
	_, file, _, _ := runtime.Caller(0)
	return file
}()

// caller is the first frame outside gorm and this file: the query's origin.
func caller() string {
	pcs := make([]uintptr, 32)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	for {
		frame, more := frames.Next()
		if frame.File != loggerFile && !strings.Contains(frame.File, "gorm.io/") {
			return fmt.Sprintf("%s:%d", frame.File, frame.Line)
		}
		if !more {
			return ""
		}
	}
}

// SetDebug switches statement tracing on or off for an open database.
func (d *DB) SetDebug(debug bool) {
	if l, ok := d.Config.Logger.(*sqlLogger); ok {
		l.verbose.Store(debug)
	}
}

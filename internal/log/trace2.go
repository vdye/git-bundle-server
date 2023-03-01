package log

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Trace2 environment variables
const (
	// TODO: handle GIT_TRACE2 by adding a separate output config (see zapcore
	// "AdvancedConfiguration" example:
	// https://pkg.go.dev/go.uber.org/zap#example-package-AdvancedConfiguration)
	trace2Basic string = "GIT_TRACE2"
	trace2Perf  string = "GIT_TRACE2_PERF"
	trace2Event string = "GIT_TRACE2_EVENT"
)

// Global start time
var globalStart = time.Now().UTC()

const trace2TimeFormat string = "2006-01-02T15:04:05.000000Z"

type ctxKey int

const (
	sidId ctxKey = iota
	parentRegionId
)

type trace2Region struct {
	level  int
	tStart time.Time
}

type Trace2 struct {
	logger      *zap.Logger
	lastChildId int32
}

func getTrace2WriteSyncer(envKey string) zapcore.WriteSyncer {
	tr2Output := os.Getenv(envKey)

	// Configure the output
	if tr2, err := strconv.Atoi(tr2Output); err == nil {
		// Handle numeric values
		if tr2 == 1 {
			return zapcore.Lock(os.Stderr)
		}
		// TODO: handle file handles 2-9 and unix sockets
	} else if tr2Output != "" {
		// Assume we received a path
		fileInfo, err := os.Stat(tr2Output)
		var filename string
		if err == nil && fileInfo.IsDir() {
			// If the path is an existing directory, generate a filename
			filename = fmt.Sprintf("trace2_%s.txt", globalStart.Format(trace2TimeFormat))
		} else {
			// Create leading directories
			parentDir := path.Dir(tr2Output)
			os.MkdirAll(parentDir, 0o755)
			filename = tr2Output
		}

		file, _, err := zap.Open(filename)
		if err != nil {
			panic(err)
		}
		return file
	}

	return zapcore.AddSync(io.Discard)
}

func createTrace2EventCore() zapcore.Core {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       zapcore.OmitKey,
		NameKey:        zapcore.OmitKey,
		CallerKey:      zapcore.OmitKey,
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "event",
		StacktraceKey:  zapcore.OmitKey,
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeDuration: zapcore.SecondsDurationEncoder,
	}
	encoderConfig.EncodeTime = zapcore.TimeEncoder(
		func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.UTC().Format(trace2TimeFormat))
		},
	)
	encoder := zapcore.NewJSONEncoder(encoderConfig)
	encoder = NewTr2PerfEncoder(encoderConfig)

	// Configure the output for GIT_TRACE2_EVENT
	writeSyncer := getTrace2WriteSyncer(trace2Event)

	return zapcore.NewCore(encoder, writeSyncer, zap.NewAtomicLevelAt(zap.DebugLevel))
}

func createTrace2ZapLogger() *zap.Logger {
	core := zapcore.NewTee(
		createTrace2EventCore(),
	)
	return zap.New(core, zap.ErrorOutput(zapcore.Lock(os.Stderr)), zap.WithCaller(false))
}

func NewTrace2() traceLoggerInternal {
	return &Trace2{
		logger:      createTrace2ZapLogger(),
		lastChildId: -1,
	}
}

type fieldList []zap.Field

func (l fieldList) withTime() fieldList {
	return append(l, zap.Duration("t_abs", time.Since(globalStart)))
}

func (l fieldList) withNesting(r trace2Region, includeTRel bool) fieldList {
	l = append(l, zap.Int("nesting", r.level))
	if includeTRel {
		l = append(l, zap.Duration("t_rel", time.Since(r.tStart)))
	}
	return l
}

func (l fieldList) with(f ...zap.Field) fieldList {
	return append(l, f...)
}

func getContextValue[T any](
	ctx context.Context,
	key ctxKey,
) (bool, T) {
	var value T
	haveValue := false
	valueAny := ctx.Value(key)
	if valueAny != nil {
		value, haveValue = valueAny.(T)
	}
	return haveValue, value
}

func getOrSetContextValue[T any](
	ctx context.Context,
	key ctxKey,
	newValueFunc func() T,
) (context.Context, T) {
	var value T
	haveValue, value := getContextValue[T](ctx, key)
	if !haveValue {
		value = newValueFunc()
		ctx = context.WithValue(ctx, key, value)
	}

	return ctx, value
}

func (t *Trace2) sharedFields(ctx context.Context) (context.Context, fieldList) {
	fields := fieldList{}

	// Get the session ID
	ctx, sid := getOrSetContextValue(ctx, sidId, uuid.New)
	fields = append(fields, zap.String("sid", sid.String()))

	// Hardcode the thread to "main" because Go doesn't like to share its
	// internal info about threading.
	fields = append(fields, zap.String("thread", "main"))

	// Get the caller of the function in trace2.go
	// Skip up two levels:
	// 0: this function
	// 1: the caller of this function (logStart, Error, etc.)
	// 2: the function calling this trace2 library
	_, fileName, lineNum, ok := runtime.Caller(2)
	if ok {
		fields = append(fields,
			zap.String("file", filepath.Base(fileName)),
			zap.Int("line", lineNum),
		)
	}

	return ctx, fields
}

func (t *Trace2) logStart(ctx context.Context) context.Context {
	ctx, sharedFields := t.sharedFields(ctx)

	t.logger.Info("start", sharedFields.withTime().with(
		zap.Strings("argv", os.Args),
	)...)

	return ctx
}

func (t *Trace2) logExit(ctx context.Context, exitCode int) {
	_, sharedFields := t.sharedFields(ctx)
	fields := sharedFields.with(
		zap.Int("code", exitCode),
	)
	t.logger.Info("exit", fields.withTime()...)
	t.logger.Info("atexit", fields.withTime()...)

	t.logger.Sync()
}

func (t *Trace2) Region(ctx context.Context, category string, label string) (context.Context, func()) {
	ctx, sharedFields := t.sharedFields(ctx)
	sharedFields = sharedFields.withTime()

	// Get the nesting level & increment
	hasParentRegion, nesting := getContextValue[trace2Region](ctx, parentRegionId)
	if !hasParentRegion {
		nesting = trace2Region{
			level:  0,
			tStart: time.Now(),
		}
	} else {
		nesting.level++
		nesting.tStart = time.Now()
	}
	ctx = context.WithValue(ctx, parentRegionId, nesting)

	regionFields := fieldList{
		zap.String("category", category),
		zap.String("label", label),
	}

	t.logger.Debug("region_enter", sharedFields.withNesting(nesting, false).with(regionFields...)...)
	return ctx, func() {
		t.logger.Debug("region_leave", sharedFields.withNesting(nesting, true).with(regionFields...)...)
	}
}

func (t *Trace2) ChildProcess(ctx context.Context, cmd *exec.Cmd) (func(error), func()) {
	var startTime time.Time
	_, sharedFields := t.sharedFields(ctx)

	// Get the child id by atomically incrementing the lastChildId
	childId := atomic.AddInt32(&t.lastChildId, 1)
	t.logger.Debug("child_start", sharedFields.with(
		zap.Int32("child_id", childId),
		zap.String("child_class", "?"),
		zap.Bool("use_shell", false),
		zap.Strings("argv", cmd.Args),
	)...)

	childReady := func(execError error) {
		ready := zap.String("ready", "ready")
		if execError != nil {
			ready = zap.String("ready", "error")
		}
		t.logger.Debug("child_ready", sharedFields.with(
			zap.Int32("child_id", childId),
			zap.Int("pid", cmd.Process.Pid),
			ready,
			zap.Strings("argv", cmd.Args),
		)...)
	}

	childExit := func() {
		t.logger.Debug("child_exit", sharedFields.with(
			zap.Int32("child_id", childId),
			zap.Int("pid", cmd.ProcessState.Pid()),
			zap.Int("code", cmd.ProcessState.ExitCode()),
			zap.Duration("t_rel", time.Since(startTime)),
		)...)
	}

	// Approximate the process runtime by starting the timer now
	startTime = time.Now()

	return childReady, childExit
}

func (t *Trace2) Goroutine(ctx context.Context, routine func()) {
}

func (t *Trace2) LogCommand(ctx context.Context, commandName string) context.Context {
	ctx, sharedFields := t.sharedFields(ctx)

	t.logger.Info("cmd_name", sharedFields.with(zap.String("name", commandName))...)

	return ctx
}

func (t *Trace2) Error(ctx context.Context, err error) error {
	// We only want to log the error if it's not already logged deeper in the
	// call stack.
	if _, ok := err.(loggedError); !ok {
		_, sharedFields := t.sharedFields(ctx)
		t.logger.Error("error", sharedFields.with(
			zap.String("msg", err.Error()),
			zap.String("fmt", err.Error()))...)
	}
	return loggedError(err)
}

func (t *Trace2) Errorf(ctx context.Context, format string, a ...any) error {
	// We only want to log the error if it's not already logged deeper in the
	// call stack.
	isLogged := false
	for _, fmtArg := range a {
		if _, ok := fmtArg.(loggedError); ok {
			isLogged = true
			break
		}
	}

	err := loggedError(fmt.Errorf(format, a...))

	if isLogged {
		_, sharedFields := t.sharedFields(ctx)
		t.logger.Info("error", sharedFields.with(
			zap.String("msg", err.Error()),
			zap.String("fmt", format))...)
	}
	return err
}

func (t *Trace2) Exit(ctx context.Context, exitCode int) {
	t.logExit(ctx, exitCode)
	os.Exit(exitCode)
}

func (t *Trace2) Fatal(ctx context.Context, err error) {
	t.Exit(ctx, 1)
}

func (t *Trace2) Fatalf(ctx context.Context, format string, a ...any) {
	t.Exit(ctx, 1)
}

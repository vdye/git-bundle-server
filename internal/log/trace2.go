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
	tr2Env_Basic string = "GIT_TRACE2"
	tr2Env_Perf  string = "GIT_TRACE2_PERF"
	tr2Env_Event string = "GIT_TRACE2_EVENT"
)

// Global start time
var globalStart = time.Now().UTC()

const trace2TimeFormat string = "2006-01-02T15:04:05.000000Z"

const (
	tr2Event_Start       string = "start"
	tr2Event_CmdName            = "cmd_name"
	tr2Event_RegionEnter        = "region_enter"
	tr2Event_RegionLeave        = "region_leave"
	tr2Event_ChildStart         = "child_start"
	tr2Event_ChildReady         = "child_ready"
	tr2Event_ChildExit          = "child_exit"
	tr2Event_Error              = "error"
	tr2Event_Exit               = "exit"
	tr2Event_AtExit             = "atexit"
)

const (
	tr2Field_Sid        string = "sid"
	tr2Field_TAbs              = "t_abs"
	tr2Field_TRel              = "t_rel"
	tr2Field_Nesting           = "nesting"
	tr2Field_Thread            = "thread"
	tr2Field_File              = "file"
	tr2Field_Line              = "line"
	tr2Field_Name              = "name"
	tr2Field_Argv              = "argv"
	tr2Field_Code              = "code"
	tr2Field_Category          = "category"
	tr2Field_Label             = "label"
	tr2Field_ChildId           = "child_id"
	tr2Field_ChildClass        = "child_class"
	tr2Field_UseShell          = "use_shell"
	tr2Field_Ready             = "ready"
	tr2Field_Pid               = "pid"
	tr2Field_Msg               = "msg"
	tr2Field_Fmt               = "fmt"
)

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
		TimeKey:    "time",
		MessageKey: "event",

		LevelKey:      zapcore.OmitKey,
		NameKey:       zapcore.OmitKey,
		CallerKey:     zapcore.OmitKey,
		FunctionKey:   zapcore.OmitKey,
		StacktraceKey: zapcore.OmitKey,

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
	writeSyncer := getTrace2WriteSyncer(tr2Env_Event)

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
	return append(l, zap.Duration(tr2Field_TAbs, time.Since(globalStart)))
}

func (l fieldList) withNesting(r trace2Region, includeTRel bool) fieldList {
	l = append(l, zap.Int(tr2Field_Nesting, r.level))
	if includeTRel {
		l = append(l, zap.Duration(tr2Field_TRel, time.Since(r.tStart)))
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
	fields = append(fields, zap.String(tr2Field_Sid, sid.String()))

	// Hardcode the thread to "main" because Go doesn't like to share its
	// internal info about threading.
	fields = append(fields, zap.String(tr2Field_Thread, "main"))

	// Get the caller of the function in trace2.go
	// Skip up two levels:
	// 0: this function
	// 1: the caller of this function (logStart, Error, etc.)
	// 2: the function calling this trace2 library
	_, fileName, lineNum, ok := runtime.Caller(2)
	if ok {
		fields = append(fields,
			zap.String(tr2Field_File, filepath.Base(fileName)),
			zap.Int(tr2Field_Line, lineNum),
		)
	}

	return ctx, fields
}

func (t *Trace2) logStart(ctx context.Context) context.Context {
	ctx, sharedFields := t.sharedFields(ctx)

	t.logger.Info(tr2Event_Start, sharedFields.withTime().with(
		zap.Strings(tr2Field_Argv, os.Args),
	)...)

	return ctx
}

func (t *Trace2) logExit(ctx context.Context, exitCode int) {
	_, sharedFields := t.sharedFields(ctx)
	fields := sharedFields.with(
		zap.Int(tr2Field_Code, exitCode),
	)
	t.logger.Info(tr2Event_Exit, fields.withTime()...)
	t.logger.Info(tr2Event_AtExit, fields.withTime()...)

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
		zap.String(tr2Field_Category, category),
		zap.String(tr2Field_Label, label),
	}

	t.logger.Debug(tr2Event_RegionEnter, sharedFields.withNesting(nesting, false).with(regionFields...)...)
	return ctx, func() {
		t.logger.Debug(tr2Event_RegionLeave, sharedFields.withNesting(nesting, true).with(regionFields...)...)
	}
}

func (t *Trace2) ChildProcess(ctx context.Context, cmd *exec.Cmd) (func(error), func()) {
	var startTime time.Time
	_, sharedFields := t.sharedFields(ctx)

	// Get the child id by atomically incrementing the lastChildId
	childId := atomic.AddInt32(&t.lastChildId, 1)
	t.logger.Debug(tr2Event_ChildStart, sharedFields.with(
		zap.Int32(tr2Field_ChildId, childId),
		zap.String(tr2Field_ChildClass, "?"),
		zap.Bool(tr2Field_UseShell, false),
		zap.Strings(tr2Field_Argv, cmd.Args),
	)...)

	childReady := func(execError error) {
		ready := zap.String(tr2Field_Ready, "ready")
		if execError != nil {
			ready = zap.String(tr2Field_Ready, "error")
		}
		t.logger.Debug(tr2Event_ChildReady, sharedFields.with(
			zap.Int32(tr2Field_ChildId, childId),
			zap.Int(tr2Field_Pid, cmd.Process.Pid),
			ready,
			zap.Strings(tr2Field_Argv, cmd.Args),
		)...)
	}

	childExit := func() {
		t.logger.Debug(tr2Event_ChildExit, sharedFields.with(
			zap.Int32(tr2Field_ChildId, childId),
			zap.Int(tr2Field_Pid, cmd.ProcessState.Pid()),
			zap.Int(tr2Field_Code, cmd.ProcessState.ExitCode()),
			zap.Duration(tr2Field_TRel, time.Since(startTime)),
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

	t.logger.Info(tr2Event_CmdName, sharedFields.with(zap.String(tr2Field_Name, commandName))...)

	return ctx
}

func (t *Trace2) Error(ctx context.Context, err error) error {
	// We only want to log the error if it's not already logged deeper in the
	// call stack.
	if _, ok := err.(loggedError); !ok {
		_, sharedFields := t.sharedFields(ctx)
		t.logger.Error(tr2Event_Error, sharedFields.with(
			zap.String(tr2Field_Msg, err.Error()),
			zap.String(tr2Field_Fmt, err.Error()))...)
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
		t.logger.Info(tr2Event_Error, sharedFields.with(
			zap.String(tr2Field_Msg, err.Error()),
			zap.String(tr2Field_Fmt, format))...)
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

package log

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	tr2PerfFileLineWidth int = 28
	tr2PerfThreadWidth   int = 24
	tr2PerfEventWidth    int = 12
	tr2PerfCategoryWidth int = 12
)

type tr2PerfEncoder struct {
	zapcore.Encoder
	config     zapcore.EncoderConfig
	bufferpool buffer.Pool
}

func NewTr2PerfEncoder(config zapcore.EncoderConfig) zapcore.Encoder {
	return &tr2PerfEncoder{
		Encoder:    zapcore.NewJSONEncoder(config),
		config:     config,
		bufferpool: buffer.NewPool(),
	}
}

func (t *tr2PerfEncoder) Clone() zapcore.Encoder {
	return NewTr2PerfEncoder(t.config)
}

func getField(key string, fields []zap.Field) (zap.Field, error) {
	for _, field := range fields {
		if field.Key == key {
			return field, nil
		}
	}

	return zapcore.Field{}, fmt.Errorf("could not find log field '%s'", key)
}

func (t *tr2PerfEncoder) EncodeEntry(ent zapcore.Entry, fields []zap.Field) (*buffer.Buffer, error) {
	buffer := t.bufferpool.Get()

	// Time
	buffer.AppendString(ent.Time.Format("15:04:05.000000") + " ")

	// File and line number
	file, err := getField("file", fields)
	if err != nil {
		return nil, err
	}
	line, err := getField("line", fields)
	if err != nil {
		return nil, err
	}
	fl := fmt.Sprintf("%s:%d", file.String, line.Integer)
	if len(fl) > tr2PerfFileLineWidth {
		fl = fl[:len(fl)-3] + "..."
	}
	buffer.AppendString(fmt.Sprintf("%-*s | ", tr2PerfFileLineWidth, fl))

	// Process depth - hardcode to 0 for now
	buffer.AppendString("d0 | ")

	// Thread
	thread, err := getField("thread", fields)
	if err != nil {
		return nil, err
	}
	buffer.AppendString(fmt.Sprintf("%-*s | ", tr2PerfThreadWidth, thread.String))

	// Event
	buffer.AppendString(fmt.Sprintf("%-*s | ", tr2PerfEventWidth, ent.Message))

	// No repo, hardcode to empty
	buffer.AppendString("    | ")

	// Absolute time elapsed
	tAbs, err := getField("t_abs", fields)
	if err != nil {
		buffer.AppendString(fmt.Sprintf("%9s | ", " "))
	} else {
		buffer.AppendString(fmt.Sprintf("%9.6f | ", time.Duration(tAbs.Integer).Seconds()))
	}

	// Relative time elapsed
	tRel, err := getField("t_rel", fields)
	if err != nil {
		buffer.AppendString(fmt.Sprintf("%9s | ", " "))
	} else {
		buffer.AppendString(fmt.Sprintf("%9.6f | ", time.Duration(tRel.Integer).Seconds()))
	}

	// Category
	category, err := getField("category", fields)
	buffer.AppendString(fmt.Sprintf("%-*.*s | ", tr2PerfCategoryWidth, tr2PerfCategoryWidth, category.String))

	// General info (varies by field present)
	nesting, err := getField("nesting", fields)
	if err == nil {
		buffer.AppendString(strings.Repeat(".", int(nesting.Integer)))
	}
	label, err := getField("label", fields)
	if err == nil {
		buffer.AppendString(fmt.Sprintf("label:%s", label.String))
	}
	code, err := getField("code", fields)
	if err == nil {
		buffer.AppendString(fmt.Sprintf("code:%d", code.Integer))
	}

	buffer.AppendString("\n")
	return buffer, nil
}

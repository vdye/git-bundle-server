package log

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	tr2Enc_Perf_FileLineWidth int = 28
	tr2Enc_Perf_ThreadWidth   int = 24
	tr2Enc_Perf_EventWidth    int = 12
	tr2Enc_Perf_CategoryWidth int = 12
)

const missingFieldError string = "missing required field '%s'"

type tr2PerfEncoder struct {
	*zapcore.MapObjectEncoder
	bufferpool buffer.Pool
	isBrief    bool
}

func NewTr2PerfEncoder(isBrief bool) zapcore.Encoder {
	return &tr2PerfEncoder{
		MapObjectEncoder: zapcore.NewMapObjectEncoder(),
		bufferpool:       buffer.NewPool(),
		isBrief:          isBrief,
	}
}

func (t *tr2PerfEncoder) Clone() zapcore.Encoder {
	return &tr2PerfEncoder{
		MapObjectEncoder: zapcore.NewMapObjectEncoder(),
		bufferpool:       t.bufferpool,
		isBrief:          t.isBrief,
	}
}

func (t *tr2PerfEncoder) getField(fieldName string) (any, bool) {
	val, ok := t.MapObjectEncoder.Fields[fieldName]
	return val, ok
}

func (t *tr2PerfEncoder) getRequiredField(fieldName string) (any, error) {
	var val any
	var ok bool
	if val, ok = t.getField(fieldName); !ok {
		return nil, fmt.Errorf(missingFieldError, fieldName)
	} else {
		return val, nil
	}
}

func (t *tr2PerfEncoder) getEventLog(event string, fields []zap.Field) (string, error) {
	switch event {
	case tr2Event_CmdName:
		if val, err := t.getRequiredField(tr2Field_Name); err != nil {
			return "", err
		} else {
			return val.(string), nil
		}

	case tr2Event_Error:
		if val, ok := t.getField(tr2Field_Msg); !ok {
			return "", nil
		} else {
			return val.(string), nil
		}

	case tr2Event_ChildStart:
		var childId, childClass, argv any
		var argvBuf []byte
		var err error
		if childId, err = t.getRequiredField(tr2Field_ChildId); err != nil {
			return "", err
		}
		if childClass, err = t.getRequiredField(tr2Field_ChildClass); err != nil {
			return "", err
		}
		if argv, err = t.getRequiredField(tr2Field_Argv); err != nil {
			return "", err
		}
		if argvBuf, err = json.Marshal(argv); err != nil {
			// TODO: don't marshal as JSON - no quotes, unless arg has spaces
			return "", fmt.Errorf("could not format argument array '%s'", tr2Field_Argv)
		}
		return fmt.Sprintf("[ch%d] class:%s argv:%s", childId, childClass, string(argvBuf)), nil

	case tr2Event_ChildReady:
		var childId, pid, ready any
		var err error
		if childId, err = t.getRequiredField(tr2Field_ChildId); err != nil {
			return "", err
		}
		if pid, err = t.getRequiredField(tr2Field_Pid); err != nil {
			return "", err
		}
		if ready, err = t.getRequiredField(tr2Field_Ready); err != nil {
			return "", err
		}
		return fmt.Sprintf("[ch%d] pid:%d ready:%s", childId, pid, ready), nil

	case tr2Event_ChildExit:
		var childId, pid, code any
		var err error
		if childId, err = t.getRequiredField(tr2Field_ChildId); err != nil {
			return "", err
		}
		if pid, err = t.getRequiredField(tr2Field_Pid); err != nil {
			return "", err
		}
		if code, err = t.getRequiredField(tr2Field_Code); err != nil {
			return "", err
		}
		return fmt.Sprintf("[ch%d] pid:%d code:%d", childId, pid, code), nil

	case tr2Event_RegionEnter:
		fallthrough
	case tr2Event_RegionLeave:
		if val, ok := t.getField(tr2Field_Label); !ok {
			return "", nil
		} else {
			return fmt.Sprintf("label:%s", val), nil
		}

	case tr2Event_Exit:
		fallthrough
	case tr2Event_AtExit:
		if val, err := t.getRequiredField(tr2Field_Code); err != nil {
			return "", err
		} else {
			return fmt.Sprintf("code:%d", val), nil
		}

	default:
		return "", nil
	}
}

func (t *tr2PerfEncoder) EncodeEntry(ent zapcore.Entry, fields []zap.Field) (*buffer.Buffer, error) {
	var val any
	var ok bool
	var err error
	event := ent.Message

	// First, validate all required fields are present
	for _, field := range fields {
		field.AddTo(t)
	}

	buffer := t.bufferpool.Get()

	if !t.isBrief {
		// Time (*not* UTC)
		buffer.AppendString(ent.Time.Format("15:04:05.000000") + " ")

		// File and line number
		var file, line any
		if file, err = t.getRequiredField(tr2Field_File); err != nil {
			return nil, err
		}
		if line, err = t.getRequiredField(tr2Field_Line); err != nil {
			return nil, err
		}

		fl := fmt.Sprintf("%s:%d", file, line)
		if len(fl) > tr2Enc_Perf_FileLineWidth {
			fl = fl[:len(fl)-3] + "..."
		}
		buffer.AppendString(fmt.Sprintf("%-*s | ", tr2Enc_Perf_FileLineWidth, fl))
	}

	// Process depth - hardcode to 0 for now
	buffer.AppendString("d0 | ")

	// Thread
	if val, err = t.getRequiredField(tr2Field_Thread); err != nil {
		return nil, err
	}
	buffer.AppendString(fmt.Sprintf("%-*s | ", tr2Enc_Perf_ThreadWidth, val))

	// Event
	buffer.AppendString(fmt.Sprintf("%-*s | ", tr2Enc_Perf_EventWidth, event))

	// No repo, hardcode to empty
	buffer.AppendString("    | ")

	// Absolute time elapsed
	if val, ok := t.getField(tr2Field_TAbs); !ok {
		buffer.AppendString(fmt.Sprintf("%9s | ", " "))
	} else {
		buffer.AppendString(fmt.Sprintf("%9.6f | ", val.(time.Duration).Seconds()))
	}

	// Relative time elapsed
	if val, ok := t.getField(tr2Field_TRel); !ok {
		buffer.AppendString(fmt.Sprintf("%9s | ", " "))
	} else {
		buffer.AppendString(fmt.Sprintf("%9.6f | ", val.(time.Duration).Seconds()))
	}

	// Category
	if val, ok = t.getField(tr2Field_Category); !ok {
		val = ""
	}
	buffer.AppendString(fmt.Sprintf("%-*.*s | ", tr2Enc_Perf_CategoryWidth, tr2Enc_Perf_CategoryWidth, val))

	// General info (varies by field present)
	if val, ok := t.getField(tr2Field_Nesting); ok {
		buffer.AppendString(strings.Repeat(".", val.(int)))
	}
	logMsg, err := t.getEventLog(event, fields)
	if err != nil {
		return nil, err
	}
	buffer.AppendString(logMsg)

	buffer.AppendString("\n")
	return buffer, nil
}

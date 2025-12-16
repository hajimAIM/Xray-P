package log

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sirupsen/logrus"
)

type ModernFormatter struct {
	// TimestampFormat sets the format used for marshaling timestamps.
	TimestampFormat string
}

func (f *ModernFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	var b *bytes.Buffer
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = time.StampMilli
	}

	// Styles
	timestampStyle := lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("241"))
	levelStyle := lipgloss.NewStyle().Bold(true)
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	callerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Italic(true)

	// Level Color
	var levelStr string
	switch entry.Level {
	case logrus.DebugLevel:
		levelStyle = levelStyle.Foreground(lipgloss.Color("63")).SetString("DEBG")
	case logrus.InfoLevel:
		levelStyle = levelStyle.Foreground(lipgloss.Color("86")).SetString("INFO")
	case logrus.WarnLevel:
		levelStyle = levelStyle.Foreground(lipgloss.Color("192")).SetString("WARN")
	case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
		levelStyle = levelStyle.Foreground(lipgloss.Color("204")).SetString("ERRO")
	default:
		levelStyle = levelStyle.SetString(strings.ToUpper(entry.Level.String())[:4])
	}
	levelStr = levelStyle.String()

	// Timestamp
	timeStr := timestampStyle.Render(entry.Time.Format(timestampFormat))

	// Message
	msg := msgStyle.Render(entry.Message)

	// Caller (if enabled)
	var callerStr string
	if entry.HasCaller() {
		funcName := entry.Caller.Function
		file := entry.Caller.File
		line := entry.Caller.Line
		// shorten file path
		parts := strings.Split(file, "/")
		if len(parts) > 2 {
			file = strings.Join(parts[len(parts)-2:], "/")
		}
		callerStr = callerStyle.Render(fmt.Sprintf(" %s:%d %s()", file, line, funcName))
	}

	// Write to buffer
	fmt.Fprintf(b, "%s %s %s%s\n", timeStr, levelStr, msg, callerStr)

	return b.Bytes(), nil
}

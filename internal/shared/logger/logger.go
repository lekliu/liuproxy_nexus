package logger

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"liuproxy_gateway/internal/shared/types"
)

// Init initializes the global logger for both the main application (zerolog)
// and the internal xray-core library.
func Init(cfg types.LogConf) error {
	// --- Part 1: Initialize zerolog for the main application ---
	levelStr := strings.ToLower(cfg.Level)
	level, err := zerolog.ParseLevel(levelStr)
	if err != nil {
		level = zerolog.InfoLevel
		fmt.Printf("Unknown log level '%s', defaulting to 'info' for zerolog\n", levelStr)
	}

	// Force all timestamps to be in UTC.
	zerolog.TimestampFunc = func() time.Time {
		return time.Now().UTC()
	}

	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: "2006-01-02 15:04:05",
	}

	log.Logger = zerolog.New(consoleWriter).
		Level(level).
		With().
		Timestamp().
		Logger()

	Info().Msgf("Main logger (zerolog) initialized with level: %s", level.String())

	return nil
}

// 这对于在日志中区分不同模块或组件的输出非常有用。
func WithComponent(name string) zerolog.Logger {
	return log.Logger.With().Str("component", name).Logger()
}

// Event is a wrapper for a zerolog event.
type Event struct {
	*zerolog.Event
}

// Debug starts a new message with debug level.
func Debug() *Event {
	return &Event{log.Debug()}
}

// Info starts a new message with info level.
func Info() *Event {
	return &Event{log.Info()}
}

// Warn starts a new message with warning level.
func Warn() *Event {
	return &Event{log.Warn()}
}

// Error starts a new message with error level.
func Error() *Event {
	return &Event{log.Error()}
}

// Fatal starts a new message with fatal level. The program will exit.
func Fatal() *Event {
	return &Event{log.Fatal()}
}

// Str adds a string field to the event.
func (e *Event) Str(key, value string) *Event {
	e.Event = e.Event.Str(key, value)
	return e
}

// Int adds an integer field to the event.
func (e *Event) Int(key string, value int) *Event {
	e.Event = e.Event.Int(key, value)
	return e
}

func (e *Event) Uint16(key string, value uint16) *Event {
	e.Event = e.Event.Uint16(key, value)
	return e
}

func (e *Event) Int64(key string, value int64) *Event {
	e.Event = e.Event.Int64(key, value)
	return e
}

func (e *Event) Hex(key string, data []byte) *Event {
	e.Event = e.Event.Hex(key, data)
	return e
}

func (e *Event) Bool(key string, value bool) *Event {
	e.Event = e.Event.Bool(key, value)
	return e
}

// Err adds an error field to the event.
func (e *Event) Err(err error) *Event {
	e.Event = e.Event.Err(err)
	return e
}

// Interface adds a field with any type to the event.
func (e *Event) Interface(key string, value interface{}) *Event {
	e.Event = e.Event.Interface(key, value)
	return e
}

// Msgf sends the event with a formatted message.
// This is a convenience method and is less performant than using structured fields.
func (e *Event) Msgf(format string, v ...interface{}) {
	e.Event.Msgf(format, v...)
}

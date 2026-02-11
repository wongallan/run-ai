// Package output provides an event-based output system for the rai CLI.
//
// It supports three output modes:
//   - Default: all events (reasoning, commands, outputs, errors) stream to console.
//   - Silent (-silent): only errors and the final response reach the console.
//   - Logged (-log): every event is also written to a timestamped log file in .rai/log/.
//
// Silent and Log can be combined: everything goes to the log, only the final
// response and errors appear on the console.
package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	raiDirName = ".rai"
	logDirName = "log"
)

// EventKind identifies the type of output event.
type EventKind string

const (
	EventAI        EventKind = "AI"     // Assistant message text
	EventReasoning EventKind = "REASON" // Reasoning summary text
	EventCMD       EventKind = "CMD"    // Terminal command being executed
	EventOUT       EventKind = "OUT"    // Terminal command output
	EventERR       EventKind = "ERR"    // Error or warning
)

// Sink receives output events and writes them to console and/or a log file.
// All methods are safe for concurrent use.
type Sink struct {
	mu      sync.Mutex
	console io.Writer
	logFile *os.File
	silent  bool
	now     func() time.Time
}

// Options configures how a Sink behaves.
type Options struct {
	Silent  bool      // Suppress console output except errors and final response.
	Log     bool      // Write all events to a log file in .rai/log/.
	BaseDir string    // Working directory root (for .rai/log/).
	Console io.Writer // Writer for console output (typically os.Stdout).

	// Now overrides the clock for deterministic testing.  When nil time.Now is used.
	Now func() time.Time
}

// NewSink creates an output sink.  When Log is true the .rai/log/ directory and
// a new log file are created immediately so callers get an early error if the path
// is not writable.
func NewSink(opts Options) (*Sink, error) {
	s := &Sink{
		console: opts.Console,
		silent:  opts.Silent,
		now:     opts.Now,
	}
	if s.now == nil {
		s.now = time.Now
	}

	if opts.Log {
		logDir := filepath.Join(opts.BaseDir, raiDirName, logDirName)
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating log directory: %w", err)
		}
		ts := s.now().Format("20060102.150405")
		logPath := filepath.Join(logDir, fmt.Sprintf("rai-log-%s.log", ts))
		f, err := os.Create(logPath)
		if err != nil {
			return nil, fmt.Errorf("creating log file: %w", err)
		}
		s.logFile = f
	}
	return s, nil
}

// WriteHeader writes the session preamble to the log file.
// It is a no-op when logging is disabled.
func (s *Sink) WriteHeader(args map[string]string, agentContent, prompt string) {
	if s.logFile == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var b strings.Builder
	b.WriteString("=== RAI Session Log ===\n")
	b.WriteString(fmt.Sprintf("Started: %s\n\n", s.now().Format("2006-01-02 15:04:05")))

	b.WriteString("--- Command Line Arguments ---\n")
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("%s: %s\n", k, args[k]))
	}
	b.WriteString("\n")

	if agentContent != "" {
		b.WriteString("--- Agent File ---\n")
		b.WriteString(agentContent)
		if !strings.HasSuffix(agentContent, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("--- User Prompt ---\n")
	b.WriteString(prompt)
	b.WriteString("\n\n")

	b.WriteString("--- Session Log ---\n")

	fmt.Fprint(s.logFile, b.String())
}

// Emit writes an event to active outputs.
// In silent mode only EventERR reaches the console; all events always reach the log.
func (s *Sink) Emit(kind EventKind, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Console: show everything unless silent (errors always shown).
	if !s.silent || kind == EventERR {
		fmt.Fprintf(s.console, "[%s] %s\n", kind, text)
	}

	// Log file: always record with timestamp.
	if s.logFile != nil {
		ts := s.now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(s.logFile, "[%s] [%s] %s\n", ts, kind, text)
	}
}

// EmitLog writes an event only to the log file, if logging is enabled.
func (s *Sink) EmitLog(kind EventKind, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.logFile != nil {
		ts := s.now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(s.logFile, "[%s] [%s] %s\n", ts, kind, text)
	}
}

// BeginAIStream writes the AI prefix to the console for inline streaming.
func (s *Sink) BeginAIStream() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.silent {
		return
	}
	fmt.Fprint(s.console, "[AI] ")
}

// EmitAIChunk writes streamed AI text without a prefix or newline.
func (s *Sink) EmitAIChunk(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.silent {
		return
	}
	fmt.Fprint(s.console, text)
}

// EndAIStream ensures the streamed AI output ends with a newline.
func (s *Sink) EndAIStream(finalText string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.silent {
		return
	}
	if !strings.HasSuffix(finalText, "\n") {
		fmt.Fprintln(s.console)
	}
}

// EmitFinal writes the final response.  It is always printed to the console,
// even in silent mode, and is recorded in the log.
func (s *Sink) EmitFinal(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fmt.Fprint(s.console, text)
	if !strings.HasSuffix(text, "\n") {
		fmt.Fprintln(s.console)
	}

	if s.logFile != nil {
		ts := s.now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(s.logFile, "[%s] [AI] %s\n", ts, text)
	}
}

// Close flushes and closes the log file.  It is safe to call multiple times.
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logFile != nil {
		err := s.logFile.Close()
		s.logFile = nil
		return err
	}
	return nil
}

// LogPath returns the log file path, or "" when logging is disabled.
func (s *Sink) LogPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logFile == nil {
		return ""
	}
	return s.logFile.Name()
}

// IsSilent reports whether the sink is configured for silent console output.
func (s *Sink) IsSilent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.silent
}

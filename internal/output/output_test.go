package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a deterministic time for reproducible tests.
func fixedClock() time.Time {
	return time.Date(2024, 3, 15, 14, 30, 22, 0, time.UTC)
}

func nowFunc() func() time.Time {
	return func() time.Time { return fixedClock() }
}

// --- Default mode tests ---

func TestEmitDefaultMode(t *testing.T) {
	var buf bytes.Buffer
	sink, err := NewSink(Options{Console: &buf, Now: nowFunc()})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close()

	sink.Emit(EventAI, "thinking about it")
	sink.Emit(EventCMD, "ls -la")
	sink.Emit(EventOUT, "file1.txt")
	sink.Emit(EventERR, "something went wrong")

	out := buf.String()
	for _, want := range []string{
		"[AI] thinking about it",
		"[CMD] ls -la",
		"[OUT] file1.txt",
		"[ERR] something went wrong",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in console output, got:\n%s", want, out)
		}
	}
}

// --- Silent mode tests ---

func TestEmitSilentSuppressesNonErrors(t *testing.T) {
	var buf bytes.Buffer
	sink, err := NewSink(Options{Console: &buf, Silent: true, Now: nowFunc()})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close()

	sink.Emit(EventAI, "thinking")
	sink.Emit(EventCMD, "ls")
	sink.Emit(EventOUT, "output")
	sink.Emit(EventERR, "error msg")

	out := buf.String()
	for _, suppressed := range []string{"[AI]", "[CMD]", "[OUT]"} {
		if strings.Contains(out, suppressed) {
			t.Errorf("expected %s suppressed in silent mode, got:\n%s", suppressed, out)
		}
	}
	if !strings.Contains(out, "[ERR] error msg") {
		t.Errorf("expected ERR to show in silent mode, got:\n%s", out)
	}
}

func TestEmitFinalAlwaysShownInSilentMode(t *testing.T) {
	var buf bytes.Buffer
	sink, err := NewSink(Options{Console: &buf, Silent: true, Now: nowFunc()})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close()

	sink.EmitFinal("The capital of France is Paris.")

	if !strings.Contains(buf.String(), "The capital of France is Paris.") {
		t.Errorf("expected final response in console output, got:\n%s", buf.String())
	}
}

func TestEmitFinalAppendsNewline(t *testing.T) {
	var buf bytes.Buffer
	sink, err := NewSink(Options{Console: &buf, Now: nowFunc()})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close()

	sink.EmitFinal("no newline")
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("expected trailing newline, got %q", buf.String())
	}
}

// --- Log file tests ---

func TestLogFileCreation(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	sink, err := NewSink(Options{
		Console: &buf,
		Log:     true,
		BaseDir: dir,
		Now:     nowFunc(),
	})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}

	logPath := sink.LogPath()
	expected := filepath.Join(dir, ".rai", "log", "rai-log-20240315.143022.log")
	if logPath != expected {
		t.Fatalf("log path = %q, want %q", logPath, expected)
	}

	sink.Emit(EventAI, "thinking")
	sink.Emit(EventCMD, "echo hello")
	sink.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	content := string(data)
	for _, want := range []string{
		"[2024-03-15 14:30:22.000] [AI] thinking",
		"[2024-03-15 14:30:22.000] [CMD] echo hello",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in log file, got:\n%s", want, content)
		}
	}
}

func TestLogPathEmptyWhenNoLogging(t *testing.T) {
	var buf bytes.Buffer
	sink, err := NewSink(Options{Console: &buf, Now: nowFunc()})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}
	defer sink.Close()

	if p := sink.LogPath(); p != "" {
		t.Errorf("expected empty log path, got %q", p)
	}
}

// --- Log header tests ---

func TestWriteHeader(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	sink, err := NewSink(Options{
		Console: &buf,
		Log:     true,
		BaseDir: dir,
		Now:     nowFunc(),
	})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}

	sink.WriteHeader(
		map[string]string{"model": "gpt-4", "log": "true"},
		"You are a code reviewer.",
		"review this code",
	)
	sink.Close()

	data, err := os.ReadFile(sink.LogPath())
	if err != nil {
		// LogPath returns "" after Close; read using expected path.
		data, err = os.ReadFile(filepath.Join(dir, ".rai", "log", "rai-log-20240315.143022.log"))
		if err != nil {
			t.Fatalf("read log: %v", err)
		}
	}

	content := string(data)
	for _, want := range []string{
		"=== RAI Session Log ===",
		"Started: 2024-03-15 14:30:22",
		"model: gpt-4",
		"log: true",
		"--- Agent File ---",
		"You are a code reviewer.",
		"--- User Prompt ---",
		"review this code",
		"--- Session Log ---",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in log header, got:\n%s", want, content)
		}
	}
}

func TestWriteHeaderNoAgent(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	sink, err := NewSink(Options{
		Console: &buf,
		Log:     true,
		BaseDir: dir,
		Now:     nowFunc(),
	})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}

	sink.WriteHeader(map[string]string{}, "", "hello")
	sink.Close()

	data, _ := os.ReadFile(filepath.Join(dir, ".rai", "log", "rai-log-20240315.143022.log"))
	content := string(data)
	if strings.Contains(content, "--- Agent File ---") {
		t.Errorf("expected no agent section when agent is empty, got:\n%s", content)
	}
}

func TestWriteHeaderNoOpWithoutLog(t *testing.T) {
	var buf bytes.Buffer
	sink, _ := NewSink(Options{Console: &buf, Now: nowFunc()})
	defer sink.Close()

	// Should not panic or error.
	sink.WriteHeader(map[string]string{"key": "val"}, "agent", "prompt")
}

// --- Combined silent + log tests ---

func TestSilentAndLogCombined(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	sink, err := NewSink(Options{
		Console: &buf,
		Silent:  true,
		Log:     true,
		BaseDir: dir,
		Now:     nowFunc(),
	})
	if err != nil {
		t.Fatalf("NewSink: %v", err)
	}

	sink.Emit(EventAI, "reasoning")
	sink.Emit(EventCMD, "make build")
	sink.EmitFinal("build succeeded")
	logPath := sink.LogPath()
	sink.Close()

	// Console: only final and errors.
	console := buf.String()
	if strings.Contains(console, "[AI]") {
		t.Errorf("AI should be suppressed on console in silent mode")
	}
	if strings.Contains(console, "[CMD]") {
		t.Errorf("CMD should be suppressed on console in silent mode")
	}
	if !strings.Contains(console, "build succeeded") {
		t.Errorf("final response should appear on console")
	}

	// Log: everything present.
	data, _ := os.ReadFile(logPath)
	logContent := string(data)
	if !strings.Contains(logContent, "[AI] reasoning") {
		t.Errorf("AI event missing from log")
	}
	if !strings.Contains(logContent, "[CMD] make build") {
		t.Errorf("CMD event missing from log")
	}
	if !strings.Contains(logContent, "[AI] build succeeded") {
		t.Errorf("final response missing from log")
	}
}

// --- Close idempotency ---

func TestCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer

	sink, _ := NewSink(Options{Console: &buf, Log: true, BaseDir: dir, Now: nowFunc()})
	if err := sink.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

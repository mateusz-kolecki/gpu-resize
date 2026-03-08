package processor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mateusz/gpu-resize/internal/processor"
)

// mockProcessor is a test double that records calls and returns canned results.
type mockProcessor struct {
	name      string
	available bool
	resizeErr error
	results   []processor.FileResult
}

func (m *mockProcessor) Name() string      { return m.name }
func (m *mockProcessor) Available() bool   { return m.available }
func (m *mockProcessor) Close() error      { return nil }

func (m *mockProcessor) ResizeFile(_ context.Context, inputPath string, _ processor.Options) (processor.FileResult, error) {
	if m.resizeErr != nil {
		return processor.FileResult{InputPath: inputPath, Err: m.resizeErr}, m.resizeErr
	}
	return processor.FileResult{InputPath: inputPath, OutputPath: inputPath + ".out"}, nil
}

func (m *mockProcessor) ResizeDir(_ context.Context, _ string, _ processor.Options) (<-chan processor.Progress, error) {
	ch := make(chan processor.Progress, len(m.results)+1)
	for i, r := range m.results {
		cp := r
		ch <- processor.Progress{Done: i + 1, Total: len(m.results), Result: &cp}
	}
	close(ch)
	return ch, nil
}

// Compile-time assertion: mockProcessor implements processor.Processor.
var _ processor.Processor = (*mockProcessor)(nil)

func TestDefaultOptions_Completeness(t *testing.T) {
	opts := processor.DefaultOptions()

	if opts.Algorithm != processor.AlgorithmLanczos && opts.Algorithm != processor.AlgorithmBilinear {
		t.Errorf("unknown default algorithm: %q", opts.Algorithm)
	}
	if opts.JPEGQuality < 1 || opts.JPEGQuality > 100 {
		t.Errorf("JPEG quality out of range: %d", opts.JPEGQuality)
	}
	if opts.TargetWidth <= 0 || opts.TargetHeight <= 0 {
		t.Error("target dimensions must be positive")
	}
}

func TestMockProcessor_ResizeFile_Success(t *testing.T) {
	m := &mockProcessor{name: "mock", available: true}

	res, err := m.ResizeFile(context.Background(), "input.jpg", processor.DefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OutputPath == "" {
		t.Error("expected non-empty OutputPath")
	}
}

func TestMockProcessor_ResizeFile_Error(t *testing.T) {
	want := errors.New("disk full")
	m := &mockProcessor{name: "mock", available: true, resizeErr: want}

	res, err := m.ResizeFile(context.Background(), "input.jpg", processor.DefaultOptions())
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
	if res.Err == nil {
		t.Error("expected Result.Err to be set")
	}
}

func TestMockProcessor_ResizeDir(t *testing.T) {
	results := []processor.FileResult{
		{InputPath: "a.jpg", OutputPath: "a.out"},
		{InputPath: "b.png", OutputPath: "b.out"},
	}
	m := &mockProcessor{name: "mock", available: true, results: results}

	ch, err := m.ResizeDir(context.Background(), "/some/dir", processor.DefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for prog := range ch {
		if prog.Result != nil {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 results, got %d", count)
	}
}

func TestAlgorithmConstants(t *testing.T) {
	if processor.AlgorithmBilinear == processor.AlgorithmLanczos {
		t.Error("algorithm constants must be distinct")
	}
}

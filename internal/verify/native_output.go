package verify

import (
	"bytes"
	"fmt"
	"slices"
)

// Each stream retains a byte prefix. Returning the original write length lets
// os/exec keep draining the pipe after the retained prefix fills up.
const nativeOutputLimit = 1 << 20

type outputBuffer struct {
	buffer    bytes.Buffer
	truncated bool
}

func newOutputBuffer() *outputBuffer {
	return &outputBuffer{}
}

func (b *outputBuffer) Write(p []byte) (int, error) {
	retained := min(len(p), nativeOutputLimit-b.buffer.Len())
	_, _ = b.buffer.Write(p[:retained]) // bytes.Buffer.Write never fails.
	b.truncated = b.truncated || retained < len(p)
	return len(p), nil
}

func (b *outputBuffer) String() string {
	return b.buffer.String()
}

func outputWarnings(stdout, stderr *outputBuffer) []Warning {
	var warnings []Warning
	for _, stream := range []struct {
		name   string
		buffer *outputBuffer
	}{{"stdout", stdout}, {"stderr", stderr}} {
		if stream.buffer.truncated {
			warnings = append(warnings, Warning{Kind: WarningOutputTruncated, Message: fmt.Sprintf("native %s exceeded %d bytes; retained only the first %d bytes and drained the remainder", stream.name, nativeOutputLimit, nativeOutputLimit)})
		}
	}
	return warnings
}

// Multi-step checks share the same report budget, rather than multiplying it
// by the number of subprocesses. Each subprocess was parsed before merging.
func mergeToolOutput(previous, next toolRun) toolRun {
	stdout, stderr := newOutputBuffer(), newOutputBuffer()
	_, _ = stdout.Write([]byte(previous.Stdout))
	_, _ = stdout.Write([]byte(next.Stdout))
	_, _ = stderr.Write([]byte(previous.Stderr))
	_, _ = stderr.Write([]byte(next.Stderr))
	warnings := slices.Concat(previous.Warnings, next.Warnings)
	warnings = append(warnings, outputWarnings(stdout, stderr)...)
	return toolRun{ExitCode: next.ExitCode, Stdout: stdout.String(), Stderr: stderr.String(), Warnings: warnings}
}

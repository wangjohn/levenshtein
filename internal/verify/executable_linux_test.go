//go:build linux

package verify

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInheritedExecutableWriterFixture(t *testing.T) {
	if os.Getenv("LEVENSHTEIN_INHERITED_WRITER_FIXTURE") != "1" {
		return
	}
	writer := os.NewFile(3, "inherited-writer")
	release := os.NewFile(4, "release")
	ready := os.NewFile(5, "ready")
	defer func() { _ = writer.Close() }() // Child owns the inherited descriptors.
	defer func() { _ = release.Close() }()
	defer func() { _ = ready.Close() }()

	if _, err := fmt.Fprintln(ready, "writer held"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(release, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
}

// A deliberate ExtraFiles inheritance extends the normally brief pre-exec
// writer lifetime, making Linux's failure deterministic without timing races.
func TestInheritedWriterBlocksAtomicallyPublishedExecutable(t *testing.T) {
	fixture, binary, env := executableFixture(t)
	dir := t.TempDir()
	pending := filepath.Join(dir, "pending")
	published := filepath.Join(dir, "published")
	writer, err := os.OpenFile(pending, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	if _, err := writer.Write(binary); err != nil {
		t.Fatal(err)
	}
	releaseRead, releaseWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = releaseRead.Close()
		_ = releaseWrite.Close()
	})
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = readyRead.Close()
		_ = readyWrite.Close()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, fixture, "-test.run=^TestInheritedExecutableWriterFixture$")
	child.Env = append(os.Environ(), "LEVENSHTEIN_INHERITED_WRITER_FIXTURE=1")
	child.ExtraFiles = []*os.File{writer, releaseRead, readyWrite}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	_ = readyWrite.Close()
	if ready, err := bufio.NewReader(readyRead).ReadString('\n'); err != nil || ready != "writer held\n" {
		t.Fatalf("inherited writer readiness: %q, %v", ready, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(pending, published); err != nil {
		t.Fatal(err)
	}

	if err := runExecutableFixture(ctx, published, env); !errors.Is(err, syscall.ETXTBSY) {
		t.Fatalf("published inode with a child's writer: %v, want ETXTBSY", err)
	}
	if _, err := releaseWrite.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}

	if err := runExecutableFixture(ctx, published, env); err != nil {
		t.Fatalf("published native bytes after inherited writer closes: %v", err)
	}
}

package verify

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Keep mutations small enough that nested JSON and line attribution remain
// inexpensive. These are fuzz harness limits, not public file size limits.
const fuzzInputLimit = 16 << 10

func FuzzConfig(f *testing.F) {
	for _, seed := range []string{`{"version":1}`, `{"version":1,"targets":{"root":{"dir":".","workspace":".","inputs":["."]}}}`, `{"version":1,"unknown":true}`, `{"version":1} {}`, `{"version":`, `null`, `[]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzInputLimit {
			t.Skip()
		}
		cfg, err := Parse(data)
		if err != nil {
			return
		}
		if !json.Valid(data) {
			t.Fatal("accepted invalid JSON")
		}
		encoded, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		roundtrip, err := Parse(encoded)
		if err != nil {
			t.Fatalf("configuration round trip: %v", err)
		}
		canonical, err := json.Marshal(roundtrip)
		if err != nil || !bytes.Equal(encoded, canonical) {
			t.Fatalf("unstable configuration encoding: %v", err)
		}

		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		object["fuzz_unknown_field"] = json.RawMessage(`true`)
		unknown, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(unknown); err == nil {
			t.Fatal("accepted unknown field")
		}
		if _, err := Parse(append(bytes.Clone(data), []byte("\n{}")...)); err == nil {
			t.Fatal("accepted trailing object")
		}
	})
}

func FuzzBaseline(f *testing.F) {
	for _, seed := range []string{`{"version":1}`, `{"version":1,"findings":[]}`, `{"version":1,"findings":[{"kind":"go-lint","dir":".","file":"x.go","code":"SA1000","message":"bad x.go:2:3","count":1}]}`, `{"version":1,"findings":[{"kind":"go-lint","dir":"..","file":"../x","code":"X","message":"bad","count":1}]}`, `{"version":1,"findings":[`, `{"version":1} {}`, `null`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzInputLimit {
			t.Skip()
		}
		entries, lines, err := parseBaseline(data)
		if err != nil {
			return
		}
		if len(lines) != len(entries) {
			t.Fatal("lost entry locations")
		}
		for i, entry := range entries {
			for _, name := range []string{entry.Dir, entry.File} {
				if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(filepath.Clean(name), ".."+string(filepath.Separator)) {
					t.Fatalf("escaping baseline path %q", name)
				}
			}
			if entry.Count < 1 || lines[i] < 1 || lines[i] > bytes.Count(data, []byte("\n"))+1 {
				t.Fatal("invalid count or source line")
			}
		}
		encoded, err := (Baseline{Entries: entries}).Encode()
		if err != nil {
			t.Fatal(err)
		}
		again, _, err := parseBaseline(encoded)
		if err != nil || !slices.Equal(entries, again) {
			t.Fatalf("baseline round trip: %v", err)
		}
		if _, _, err := parseBaseline(append(bytes.Clone(data), []byte("\n{}")...)); err == nil {
			t.Fatal("accepted trailing object")
		}
	})
}

func FuzzArchiveEntry(f *testing.F) {
	f.Add("tool", []byte("binary"), uint8(tar.TypeReg))
	f.Add("../tool", []byte("escape"), uint8(tar.TypeReg))
	f.Add("tool", []byte("link"), uint8(tar.TypeSymlink))
	f.Add("/tool", []byte{}, uint8(tar.TypeReg))
	f.Fuzz(func(t *testing.T, name string, payload []byte, kind uint8) {
		if len(name) > 256 || len(payload) > 2048 {
			t.Skip()
		}
		var buffer bytes.Buffer
		compressed := gzip.NewWriter(&buffer)
		archive := tar.NewWriter(compressed)
		header := &tar.Header{Name: name, Typeflag: kind, Size: int64(len(payload)), Mode: 0700}
		if err := archive.WriteHeader(header); err != nil {
			_ = archive.Close()
			_ = compressed.Close()
			return
		}
		if _, err := archive.Write(payload); err != nil {
			_ = archive.Close()
			_ = compressed.Close()
			return
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}

		got, err := untarEntry(buffer.Bytes(), "tool", 1024)
		if err == nil && (kind != tar.TypeReg && kind != 0 || filepath.ToSlash(filepath.Clean(name)) != "tool" || len(payload) > 1024 || !bytes.Equal(got, payload)) {
			t.Fatal("returned an unexpected entry or exceeded limit")
		}
		if name == "tool" && kind == tar.TypeReg && len(payload) <= 1024 && (err != nil || !bytes.Equal(got, payload)) {
			t.Fatalf("lost valid entry: %v", err)
		}
	})
}

func FuzzArchiveBytes(f *testing.F) {
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: "tool", Mode: 0700, Size: 6, Typeflag: tar.TypeReg}); err != nil {
		f.Fatal(err)
	}
	if _, err := archive.Write([]byte("binary")); err != nil {
		f.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		f.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		f.Fatal(err)
	}
	f.Add(buffer.Bytes())
	f.Add(bytes.Clone(buffer.Bytes()[:len(buffer.Bytes())/2]))

	f.Add([]byte{})
	f.Add([]byte{0x1f, 0x8b, 8, 0})
	f.Add([]byte("not gzip"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		got, err := untarEntry(data, "tool", 1024)
		if err == nil && len(got) > 1024 {
			t.Fatal("archive exceeded binary limit")
		}
	})
}

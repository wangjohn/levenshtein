#!/bin/sh
set -eu

# The pinned generator assumes every replacement is a versioned module with
# the same path. Preserve a local telemetry replacement too, including its
# versionless directory path, in generated go.mod and go.work files.
file=cmd/codegen/generator/go/generate_module.go
grep -qF 'modRequires[minReq.New.Path]' "$file"
grep -qF 'minReq.New.Path+"@"+minReq.New.Version' "$file"
sed \
  -e 's/modRequires\[minReq.New.Path\]/modRequires[minReq.Old.Path]/g' \
  -e 's/minReq.New.Path+"@"+minReq.New.Version/strings.TrimSuffix(minReq.New.Path+"@"+minReq.New.Version, "@")/g' \
  "$file" > "$file.patched"
mv "$file.patched" "$file"
gofmt -w "$file"

# Codegen imports the engine's slog bridge; its two attribute parameters need
# the same mechanical API migration as the telemetry module.
file=engine/slog/telemetry.go
grep -qF 'attrs ...log.KeyValue' "$file"
sed \
  -e 's|"go.opentelemetry.io/otel/log"|"go.opentelemetry.io/otel/attribute"|' \
  -e 's/log.KeyValue/attribute.KeyValue/g' \
  "$file" > "$file.patched"
mv "$file.patched" "$file"
gofmt -w "$file"

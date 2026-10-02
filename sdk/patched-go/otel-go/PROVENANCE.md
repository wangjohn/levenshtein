# Patched Dagger telemetry module

Source: [`github.com/dagger/otel-go` v1.43.0](https://github.com/dagger/otel-go/tree/v1.43.0).
Upstream commit: `a9e4d7ae7eb276fe94a2aebc27d08673d50f66b3`.

Archive checksum: `h1:AYCnAamWmxtSxigWPTgC+8EWqiWPcDZEegh8y05gdJ8=`.
Upstream manifest checksum: `h1:83CTuXi70zcx1kaym5buqmb7RNzg1E9dEiQSFyLbLdU=`.

The unmodified package files and `LICENSE` came from the Go module archive.

The patch changes only `logging.go` and `transform.go`: OpenTelemetry logging
v0.21.0 moved `log.KeyValue` and `log.Value` to `attribute`, changed `Kind` to
`Type`, and added homogeneous slice values. Protobuf conversions retain scalar,
byte, mixed slice, and map values and support the new homogeneous slice forms.
The module manifest resolves the fixed logging exporters and compatible stable
OpenTelemetry dependencies. The added tests exercise values and writer output.

This local replacement is used by both the pinned Dagger generator and the
runner. Remove it when Dagger's telemetry fork supports the fixed logging API.

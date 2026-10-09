"""Temporary Dagger 0.21.9 Go SDK with patched OpenTelemetry dependencies."""

import posixpath

import dagger
from dagger import dag, function, object_type

DAGGER_COMMIT = "f2aefc20cf41b5ed7922df7244e0f10ddc6031f7"
# github.com/dagger/otel-go v1.43.0, matching the pinned Dagger SDK.
TELEMETRY_COMMIT = "a9e4d7ae7eb276fe94a2aebc27d08673d50f66b3"
CLIENT_COMMIT = "fdf4c34a9a67d096aaeef79630017c9c7ff8fe8e"
# Same tag and digest as goImage in runner/toolchain.json; TestPinsAgree in
# runner/main_test.go fails until they match. This module's Dagger source is
# sdk/patched-go, so runner/toolchain.json is outside its context, and the
# generator is built before any module source is at hand.
GO_IMAGE = "golang:1.27.2-trixie@sha256:e58d6f83b3416618d8bcac2b3dde1b7f7e3c4a77d25e88637f8bbae81536c48d"
# The golang.org/x/tools version the generator is built with; it must read the
# export data that GO_IMAGE's compiler writes. TestPinsAgree in
# runner/main_test.go keeps it equal to x/tools in runner/lint/go.mod.
CODEGEN_X_TOOLS = "v0.50.0"
LOG_MODULES = (
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc",
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp",
    "go.opentelemetry.io/otel/log",
    "go.opentelemetry.io/otel/sdk/log",
)


def base() -> dagger.Container:
    return (
        dag.container()
        .from_(GO_IMAGE)
        .with_env_variable("GOTOOLCHAIN", "local")
        .with_mounted_cache("/go/pkg/mod", dag.cache_volume("patched-go-mod"))
        .with_mounted_cache("/root/.cache/go-build", dag.cache_volume("patched-go-build"))
    )


def patched_dependencies(container: dagger.Container, modfile: str = "go.mod") -> dagger.Container:
    return container.with_exec([
        "go", "mod", "edit", f"-modfile={modfile}",
        *[f"-replace={module}={module}@v0.21.0" for module in LOG_MODULES],
        *[f"-require={module}@v0.21.0" for module in LOG_MODULES],
        "-require=go.opentelemetry.io/otel@v1.45.0",
        "-require=go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.45.0",
        "-require=go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.45.0",
        "-require=go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@v1.45.0",
        "-require=go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp@v1.45.0",
        "-require=google.golang.org/grpc@v1.83.2",
        "-require=golang.org/x/text@v0.41.0",
    ])


def telemetry_source() -> dagger.Directory:
    # otel-go v1.43.0 predates the log.Value -> attribute.Value migration.
    # Generate a local module so both the generator and runtime use this patch.
    source = dag.git("https://github.com/dagger/otel-go.git").commit(TELEMETRY_COMMIT).tree()
    container = (
        base().with_directory("/telemetry", source).with_workdir("/telemetry")
        .with_file("/otel-go.patch", dag.current_module().source().file("otel-go.patch"))
        .with_exec(["git", "apply", "/otel-go.patch"])
        .with_file("/telemetry/compat_test.go", dag.current_module().source().file("compat_test.go.fixture"))
    )
    return patched_dependencies(container).directory("/telemetry")


def codegen_binary() -> dagger.File:
    source = dag.git("https://github.com/dagger/dagger.git").commit(DAGGER_COMMIT).tree()
    container = (
        base().with_directory("/upstream", source).with_workdir("/upstream")
        .with_directory("/telemetry", telemetry_source())
        .with_exec(["go", "mod", "edit", "-replace=github.com/dagger/otel-go=/telemetry"])
        .with_exec([
            "sed", "-i",
            "s@go.opentelemetry.io/otel/log@go.opentelemetry.io/otel/attribute@; s/log.KeyValue/attribute.KeyValue/g",
            "engine/slog/telemetry.go",
        ])
    )

    # The generator embeds sdk/go/go.mod. Patch both that policy and the
    # generator's own dependencies; otherwise it reintroduces v0.16.0 on load.
    for modfile in ("go.mod", "sdk/go/go.mod"):
        container = patched_dependencies(container, modfile)

    # The generator type-checks the module from go list export data. Go 1.27.2
    # writes export data version 5, which golang.org/x/tools before v0.50.0
    # (Dagger pins v0.45.0) cannot read; every parameter type then comes out
    # as `any` and the generated dagger.gen.go does not compile.
    container = container.with_exec([
        "go", "mod", "edit", f"-require=golang.org/x/tools@{CODEGEN_X_TOOLS}",
    ])

    return (
        container.with_exec([
            "go", "build", "-mod=mod", "-o", "/codegen", "./cmd/codegen",
        ])
        .file("/codegen")
    )


@object_type
class PatchedGo:
    async def prepared(
        self, mod_source: dagger.ModuleSource, introspection_json: dagger.File
    ) -> dagger.Container:
        subpath = await mod_source.source_subpath()
        source = mod_source.context_directory().without_file(
            posixpath.join(subpath, "dagger.gen.go")
        )
        return (
            base()
            .with_file("/usr/local/bin/codegen", codegen_binary())
            .with_directory("/src", source)
            .with_file("/schema.json", introspection_json)
            .with_workdir(posixpath.join("/src", subpath))
            .with_directory(posixpath.join("/src", subpath, "internal/telemetry"), telemetry_source())
            .with_exec(["go", "mod", "edit", "-replace=github.com/dagger/otel-go=./internal/telemetry"])
        )

    async def arguments(self, mod_source: dagger.ModuleSource) -> list[str]:
        return [
            "--module-source-path", posixpath.join("/src", await mod_source.source_subpath()),
            "--module-name", await mod_source.module_original_name(),
            "--introspection-json-path", "/schema.json",
            "--lib-version", CLIENT_COMMIT,
        ]

    async def generated(
        self, mod_source: dagger.ModuleSource, introspection_json: dagger.File
    ) -> dagger.Container:
        container = await self.prepared(mod_source, introspection_json)
        return container.with_exec([
            "codegen", "generate-module", "--output", "/src",
            *await self.arguments(mod_source),
        ])

    @function
    async def codegen(
        self, mod_source: dagger.ModuleSource, introspection_json: dagger.File
    ) -> dagger.GeneratedCode:
        container = await self.generated(mod_source, introspection_json)
        return (
            dag.generated_code(container.directory("/src"))
            .with_vcs_generated_paths(["dagger.gen.go", "internal/dagger/**", "internal/telemetry/**"])
            .with_vcs_ignored_paths(["dagger.gen.go", "internal/dagger", "internal/telemetry", ".env"])
        )

    @function
    async def module_types(
        self, mod_source: dagger.ModuleSource, introspection_json: dagger.File,
        output_file_path: str,
    ) -> dagger.Container:
        container = await self.prepared(mod_source, introspection_json)
        container = container.without_directory(
            posixpath.join("/src", await mod_source.source_subpath(), "internal/dagger")
        )
        # The Go generator requires a relative filename, whereas Dagger's
        # custom SDK contract supplies an absolute output path.
        return container.with_entrypoint([
            "sh", "-ec",
            (
                'output="$1"; shift; '
                'codegen generate-typedefs --output typedefs.json "$@"; '
                'cp typedefs.json "$output"'
            ),
            "generate-typedefs", output_file_path,
            *await self.arguments(mod_source),
        ])

    @function
    async def module_runtime(
        self, mod_source: dagger.ModuleSource, introspection_json: dagger.File
    ) -> dagger.Container:
        container = await self.generated(mod_source, introspection_json)
        return (
            container.with_exec(["go", "build", "-ldflags", "-s -w", "-o", "/runtime", "."])
            .without_mount("/go/pkg/mod")
            .without_mount("/root/.cache/go-build")
            .with_workdir("/scratch")
            .with_entrypoint(["/runtime"])
        )

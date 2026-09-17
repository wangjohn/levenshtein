"""Temporary Dagger 0.21.9 Go SDK with patched OpenTelemetry dependencies."""

import posixpath

import dagger
from dagger import dag, function, object_type

DAGGER_COMMIT = "f2aefc20cf41b5ed7922df7244e0f10ddc6031f7"
CLIENT_COMMIT = "fdf4c34a9a67d096aaeef79630017c9c7ff8fe8e"
GO_IMAGE = "golang:1.27.1-trixie@sha256:9baa6b4187bbb98d240372a8a235ac0bb6b5ddd52bba1431dc2f7c0705862728"
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


def codegen_binary() -> dagger.File:
    source = dag.git("https://github.com/dagger/dagger.git").commit(DAGGER_COMMIT).tree()
    container = base().with_directory("/upstream", source).with_workdir("/upstream")

    # The generator embeds sdk/go/go.mod. Patch both that policy and the
    # generator's own dependencies; otherwise it reintroduces v0.16.0 on load.
    for modfile in ("go.mod", "sdk/go/go.mod"):
        container = container.with_exec([
            "go", "mod", "edit", f"-modfile={modfile}",
            *[f"-replace={module}={module}@v0.20.0" for module in LOG_MODULES],
            "-require=go.opentelemetry.io/otel@v1.44.0",
            "-require=go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.44.0",
            "-require=go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp@v1.44.0",
            "-require=google.golang.org/grpc@v1.83.2",
            "-require=golang.org/x/text@v0.41.0",
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
            .with_vcs_generated_paths(["dagger.gen.go", "internal/dagger/**"])
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
            'output="$1"; shift; '
            'codegen generate-typedefs --output typedefs.json "$@"; '
            'cp typedefs.json "$output"',
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

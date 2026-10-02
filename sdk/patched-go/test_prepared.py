"""Exercise dependency injection when Dagger omits sibling source files."""

import unittest
from unittest.mock import AsyncMock, MagicMock, patch

import patched_go


class PreparedTests(unittest.IsolatedAsyncioTestCase):
    async def test_filtered_context_gets_telemetry_replacement(self):
        for subpath in ("runner", "nested/runner"):
            with self.subTest(subpath=subpath):
                container = MagicMock()
                for method in ("with_file", "with_directory", "with_workdir"):
                    getattr(container, method).return_value = container
                filtered_context = MagicMock()
                source = MagicMock()
                source.source_subpath = AsyncMock(return_value=subpath)
                source.context_directory.return_value = filtered_context
                telemetry = object()
                dag = MagicMock()
                dag.current_module.return_value.source.return_value.directory.return_value = telemetry

                with patch.object(patched_go, "dag", dag), patch.object(
                    patched_go, "base", return_value=container
                ), patch.object(patched_go, "codegen_binary"):
                    result = await patched_go.PatchedGo().prepared(source, object())

                self.assertIs(result, container)
                container.with_directory.assert_any_call(
                    "/src/" + subpath.rsplit("/", 1)[0] + "/sdk/patched-go/otel-go"
                    if "/" in subpath else "/src/sdk/patched-go/otel-go",
                    telemetry,
                )
                container.with_directory.assert_any_call(
                    "/src", filtered_context.without_file.return_value
                )
                container.with_workdir.assert_called_once_with("/src/" + subpath)
                dag.current_module.return_value.source.return_value.directory.assert_called_once_with("otel-go")


if __name__ == "__main__":
    unittest.main()

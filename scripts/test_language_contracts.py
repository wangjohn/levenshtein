"""Exercise real Rust/Python tools through separate CLI processes."""
import copy
import json
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
BINARY = Path(sys.argv[1]).resolve()


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def exercise(language, temporary):
    source = temporary / language
    shutil.copytree(ROOT / "tests" / "fixtures" / language, source,
                    ignore=shutil.ignore_patterns("target", ".venv", "__pycache__", ".pytest_cache", "test-executions.txt"))
    cache = temporary / (language + "-cache")
    measurements = {}

    def run(label, name="branch", success=True):
        started = time.monotonic()
        process = subprocess.run([str(BINARY), name, "--source", str(source), "--shared", str(ROOT),
                                  "--cache-dir", str(cache)], capture_output=True, text=True, timeout=180)
        measurements[label] = round((time.monotonic() - started) * 1000, 1)
        require(bool(process.stdout), f"{language}/{label}: no report: {process.stderr}")
        report = json.loads(process.stdout)
        require((process.returncode == 0) == success, f"{language}/{label}: {process.returncode}\n{process.stdout}\n{process.stderr}")
        require((report["status"] == "passed") == success, f"{language}/{label}: incorrect gate")
        return report["results"]

    def status(results, expected):
        require(all(result["cache"]["status"] == expected for result in results), f"{language}: expected {expected}: {results}")

    first = run("cold")
    status(first, "miss")
    if language == "python":
        require(first[0]["stages"][0]["reused"] is False and first[1]["stages"][0]["reused"] is True,
                "Python lint and pytest did not share preparation")
    trace = source / "test-executions.txt"
    require(trace.read_text().count("executed") == 1, "cold verification did not execute tests")

    warm = run("warm")
    status(warm, "hit")
    require(trace.read_text().count("executed") == 1, "cache hit reexecuted tests")
    require(warm[-1]["verified_at"] == first[-1]["verified_at"], "cache hit changed original verification time")

    (source / "README.md").write_text("An unrelated documentation edit.\n")
    status(run("unrelated_edit"), "hit")

    fresh = run("fresh", "audit")
    status(fresh, "fresh")
    require(trace.read_text().count("executed") == 2, "fresh audit reused a test verdict")
    require(all(stage["reused"] for result in fresh for stage in result.get("stages", [])),
            f"{language}: fresh audit discarded compatible setup/build: {fresh}")
    status(run("after_fresh"), "hit")

    editable = source / ("crates/app/src/lib.rs" if language == "rust" else "tests/test_sample.py")
    comment = "\n// input edit\n" if language == "rust" else "\n# input edit\n"
    editable.write_text(editable.read_text() + comment)
    changed = run("small_edit")
    status(changed, "miss")
    if language == "python":
        require(all(result["stages"][0]["reused"] for result in changed), "test edit invalidated Python dependencies")

    config_path = source / "levenshtein.json"
    config = json.loads(config_path.read_text())
    variant = copy.deepcopy(config)
    if language == "rust":
        variant["checks"]["tests"]["command"]["args"] += ["--features", "extra"]
        variant["checks"]["tests"]["command"]["rerun_args"] += ["--features", "extra"]
        variant["builds"]["tests"]["command"] += ["--features", "extra"]
    else:
        variant["checks"]["tests"]["command"]["args"] += ["-k", "test_add"]
        variant["checks"]["tests"]["command"]["rerun_args"] += ["-k", "test_add"]
    config_path.write_text(json.dumps(variant))
    selected = run("variant")
    require(selected[-1]["cache"]["status"] == "miss", "execution variant reused another scope")
    require(selected[-1]["cache"]["key"] != changed[-1]["cache"]["key"], "variant key did not change")
    config_path.write_text(json.dumps(config))

    # Root manifests/lockfiles must affect checks even when cwd is a workspace member.
    lock = source / ("Cargo.lock" if language == "rust" else "uv.lock")
    lock.write_text(lock.read_text() + "\n# lockfile input changed\n")
    status(run("lockfile_edit"), "miss")

    if language == "rust":
        broken = source / "crates/arithmetic/src/lib.rs"
        broken.write_text(broken.read_text().replace("a + b", "a + b + 1"))
    else:
        broken = source / "tests/conftest.py"
        broken.write_text(broken.read_text().replace("return 3", "return 99"))

    failure = run("dependency_or_fixture_failure", success=False)
    require(failure[-1]["status"] == "failed", f"{language}: expected actual assertion failure: {failure}")
    require("assert" in (failure[-1].get("stdout", "") + failure[-1].get("stderr", "")).lower(),
            "a tooling error substituted for the intended failed assertion")

    print(json.dumps({"language": language, "milliseconds": measurements}), flush=True)


with tempfile.TemporaryDirectory(prefix="levenshtein-contracts-") as directory:
    temporary = Path(directory).resolve()
    for language in ("rust", "python"):
        exercise(language, temporary)
print("Rust and Python verification contracts passed", flush=True)

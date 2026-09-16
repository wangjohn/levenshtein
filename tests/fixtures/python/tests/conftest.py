import os
from pathlib import Path

import pytest


@pytest.fixture
def expected_total():
    return 3


def pytest_sessionfinish(session, exitstatus):
    with (Path(os.environ["LEVENSHTEIN_SOURCE"]) / "test-executions.txt").open("a") as trace:
        trace.write("executed\n")

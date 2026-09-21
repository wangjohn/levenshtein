import sys
from pathlib import Path

from sample import add

# The interpreter pin lives in .python-version, which uv reads and which is a
# declared input of this target; assert against it rather than a second copy.
PINNED_PYTHON = (Path(__file__).resolve().parents[1] / ".python-version").read_text().strip()


def test_add(expected_total):
    assert ".".join(str(part) for part in sys.version_info[:3]) == PINNED_PYTHON
    assert add(1, 2) == expected_total

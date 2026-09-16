import sys

from sample import add


def test_add(expected_total):
    assert sys.version_info[:3] == (3, 14, 7)
    assert add(1, 2) == expected_total

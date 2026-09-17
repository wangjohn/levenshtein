"""A small fixture policy check, executed in the same environment as pytest."""
import ast
from pathlib import Path

for source in Path("src").rglob("*.py"):
    tree = ast.parse(source.read_text(), filename=str(source))
    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom):
            assert all(alias.name != "*" for alias in node.names), f"{source}: wildcard import"
print("fixture lint passed")

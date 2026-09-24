"""Portable fixtures for the imported movie regression suites."""

import importlib.util
import importlib.machinery
import types
import sys
from pathlib import Path


def load_script(name: str) -> types.ModuleType:
    """Load an extensionless movie tool as a test module."""
    script = (
        Path(__file__).resolve().parents[2]
        / "skills/proving-it-works-with-a-movie/scripts"
        / name
    )
    if not script.exists():
        script = script.with_suffix(".py")
    sys.path.insert(0, str(script.parent))
    loader = importlib.machinery.SourceFileLoader(f"movie_tool_{name}", str(script))
    spec = importlib.util.spec_from_loader(loader.name, loader)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load movie tool {name!r}")
    module = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(module)
    finally:
        sys.path.pop(0)
    return module

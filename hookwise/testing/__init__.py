"""Testing utilities for hookwise hooks.

Provides test harnesses and utilities for testing hookwise hook
configurations and handlers:

- :class:`HookResult` -- Structured result with assertion methods
- :class:`HookRunner` -- Subprocess-based hook script executor
- :class:`GuardTester` -- In-process guard rule evaluator
"""

from hookwise.testing.guard_tester import GuardTester
from hookwise.testing.result import HookResult
from hookwise.testing.runner import HookRunner

__all__ = [
    "GuardTester",
    "HookResult",
    "HookRunner",
]

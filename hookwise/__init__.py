"""Hookwise: A config-driven Python framework for Claude Code hooks."""

__version__ = "0.1.0"
__all__ = [
    "__version__",
    "atomic_write_json",
    "safe_read_json",
    "ensure_state_dir",
    "get_state_dir",
    "FailOpen",
    "setup_logging",
    "ConfigEngine",
    "HooksConfig",
    "Dispatcher",
    "DispatchResult",
    "HandlerResult",
    "GuardEngine",
    "GuardRule",
    "GuardResult",
]

from hookwise.config import ConfigEngine, HooksConfig
from hookwise.dispatcher import Dispatcher, DispatchResult, HandlerResult
from hookwise.errors import FailOpen, setup_logging
from hookwise.guards import GuardEngine, GuardResult, GuardRule
from hookwise.state import atomic_write_json, ensure_state_dir, get_state_dir, safe_read_json

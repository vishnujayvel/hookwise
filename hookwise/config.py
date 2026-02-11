"""Configuration loading and validation for hookwise.

Reads hookwise YAML configuration files from project-level and global
paths, merges them with project overriding global, interpolates
environment variables, validates structure, and resolves handler
references (builtin, script, inline).

Config resolution order:
    1. ./hookwise.yaml (project-level)
    2. ~/.hookwise/config.yaml (global)
    3. Deep merge: project values override global values

Environment variable interpolation:
    ${ENV_VAR} syntax in string values is replaced with the
    corresponding environment variable value. Missing variables
    are left as empty strings.

Handler types:
    - builtin: References framework-provided handler modules
    - script: Points to custom Python or shell scripts
    - inline: Simple actions expressed directly in YAML config
"""

from __future__ import annotations

import copy
import logging
import os
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml

from hookwise.state import get_state_dir

logger = logging.getLogger("hookwise")

# All 13 Claude Code hook event types
VALID_EVENT_TYPES = frozenset({
    "UserPromptSubmit",
    "PreToolUse",
    "PostToolUse",
    "PostToolUseFailure",
    "Notification",
    "Stop",
    "SubagentStart",
    "SubagentStop",
    "PreCompact",
    "SessionStart",
    "SessionEnd",
    "PermissionRequest",
    "Setup",
})

# Valid top-level config sections
VALID_SECTIONS = frozenset({
    "version",
    "guards",
    "coaching",
    "analytics",
    "greeting",
    "sounds",
    "status_line",
    "cost_tracking",
    "transcript_backup",
    "handlers",
    "settings",
    "includes",
})

# Valid handler types
VALID_HANDLER_TYPES = frozenset({"builtin", "script", "inline"})

# Default handler timeout in seconds
DEFAULT_HANDLER_TIMEOUT = 10

# Project config filename
PROJECT_CONFIG_FILENAME = "hookwise.yaml"

# Global config path relative to state dir
GLOBAL_CONFIG_FILENAME = "config.yaml"

# Environment variable interpolation pattern: ${VAR_NAME}
_ENV_VAR_PATTERN = re.compile(r"\$\{([^}]+)\}")


@dataclass
class HooksConfig:
    """Validated hookwise configuration.

    All fields have safe defaults so that a completely empty config
    produces a usable (no-op) configuration object.

    Attributes:
        version: Config format version. Currently only 1 is supported.
        guards: List of guard handler definitions.
        coaching: Coaching module configuration.
        analytics: Analytics module configuration.
        greeting: Greeting/banner configuration.
        sounds: Sound notification configuration.
        status_line: Status line display configuration.
        cost_tracking: Cost tracking configuration.
        transcript_backup: Transcript backup configuration.
        handlers: List of general handler definitions.
        settings: Global settings (timeouts, logging, etc.).
        includes: List of recipe paths to include (resolved later).
    """

    version: int = 1
    guards: list[dict[str, Any]] = field(default_factory=list)
    coaching: dict[str, Any] = field(default_factory=dict)
    analytics: dict[str, Any] = field(default_factory=dict)
    greeting: dict[str, Any] = field(default_factory=dict)
    sounds: dict[str, Any] = field(default_factory=dict)
    status_line: dict[str, Any] = field(default_factory=dict)
    cost_tracking: dict[str, Any] = field(default_factory=dict)
    transcript_backup: dict[str, Any] = field(default_factory=dict)
    handlers: list[dict[str, Any]] = field(default_factory=list)
    settings: dict[str, Any] = field(default_factory=dict)
    includes: list[str] = field(default_factory=list)


@dataclass
class ValidationError:
    """A single validation error with a suggested fix.

    Attributes:
        path: Dot-separated path to the problematic config key.
        message: Human-readable description of the issue.
        suggestion: Optional fix suggestion for the user.
    """

    path: str
    message: str
    suggestion: str | None = None


@dataclass
class ValidationResult:
    """Result of config validation.

    Attributes:
        valid: True if the config passed all validation checks.
        errors: List of validation errors found.
    """

    valid: bool
    errors: list[ValidationError] = field(default_factory=list)


@dataclass
class ResolvedHandler:
    """A handler resolved from config with type and execution info.

    Attributes:
        name: Handler name (for logging and identification).
        handler_type: One of "builtin", "script", "inline".
        events: Set of event types this handler responds to.
        module: For builtin handlers, the module path.
        command: For script handlers, the command to execute.
        action: For inline handlers, the action definition.
        timeout: Handler-specific timeout in seconds.
        phase: Execution phase: "guard", "context", or "side_effect".
        config_raw: The original handler config dict.
    """

    name: str
    handler_type: str
    events: set[str]
    module: str | None = None
    command: str | None = None
    action: dict[str, Any] | None = None
    timeout: int = DEFAULT_HANDLER_TIMEOUT
    phase: str = "side_effect"
    config_raw: dict[str, Any] = field(default_factory=dict)


def deep_merge(base: dict[str, Any], override: dict[str, Any]) -> dict[str, Any]:
    """Deep-merge two dicts. Override values take precedence.

    Merge semantics:
    - For dict values: recursively merge (keys from both, override wins)
    - For lists and scalars: override replaces base entirely

    Neither input dict is mutated; a new dict is returned.

    Args:
        base: The base configuration (e.g., global config).
        override: The overriding configuration (e.g., project config).

    Returns:
        A new dict with merged values.
    """
    result = copy.deepcopy(base)
    for key, value in override.items():
        if (
            key in result
            and isinstance(result[key], dict)
            and isinstance(value, dict)
        ):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


def interpolate_env_vars(value: Any) -> Any:
    """Recursively interpolate ${ENV_VAR} references in config values.

    Handles strings, dicts (recursing into values), and lists
    (recursing into elements). Other types are returned unchanged.

    Missing environment variables are replaced with empty strings.

    Args:
        value: A config value (string, dict, list, or scalar).

    Returns:
        The value with all ${ENV_VAR} references replaced.
    """
    if isinstance(value, str):
        def _replace(match: re.Match[str]) -> str:
            var_name = match.group(1)
            return os.environ.get(var_name, "")

        return _ENV_VAR_PATTERN.sub(_replace, value)

    if isinstance(value, dict):
        return {k: interpolate_env_vars(v) for k, v in value.items()}

    if isinstance(value, list):
        return [interpolate_env_vars(item) for item in value]

    return value


def _resolve_handler(handler_dict: dict[str, Any], index: int) -> ResolvedHandler | None:
    """Resolve a single handler definition from config into a ResolvedHandler.

    Determines handler type (builtin, script, inline), extracts event
    bindings, timeout, and phase from the config dict.

    Args:
        handler_dict: Raw handler config dict from YAML.
        index: Position index in the handlers list (for default naming).

    Returns:
        A ResolvedHandler if resolution succeeds, None if the handler
        config is malformed or has an unsupported type.
    """
    handler_type = handler_dict.get("type")
    if handler_type not in VALID_HANDLER_TYPES:
        logger.warning(
            "Handler at index %d has unsupported type %r, skipping",
            index, handler_type,
        )
        return None

    name = handler_dict.get("name", f"handler_{index}")

    # Parse events -- can be a string or list of strings
    events_raw = handler_dict.get("events", [])
    if isinstance(events_raw, str):
        events_raw = [events_raw]
    events = set()
    for evt in events_raw:
        if evt in VALID_EVENT_TYPES:
            events.add(evt)
        elif evt == "*":
            events = set(VALID_EVENT_TYPES)
            break
        else:
            logger.warning(
                "Handler %r references unknown event type %r, skipping it",
                name, evt,
            )

    timeout = handler_dict.get("timeout", DEFAULT_HANDLER_TIMEOUT)
    if not isinstance(timeout, (int, float)):
        timeout = DEFAULT_HANDLER_TIMEOUT
    timeout = int(timeout)

    phase = handler_dict.get("phase", "side_effect")
    if phase not in ("guard", "context", "side_effect"):
        logger.warning(
            "Handler %r has invalid phase %r, defaulting to 'side_effect'",
            name, phase,
        )
        phase = "side_effect"

    resolved = ResolvedHandler(
        name=name,
        handler_type=handler_type,
        events=events,
        timeout=timeout,
        phase=phase,
        config_raw=handler_dict,
    )

    if handler_type == "builtin":
        resolved.module = handler_dict.get("module")
    elif handler_type == "script":
        resolved.command = handler_dict.get("command")
    elif handler_type == "inline":
        resolved.action = handler_dict.get("action")

    return resolved


class ConfigEngine:
    """Loads, merges, and validates hookwise configuration.

    Reads YAML config from project-level (./hookwise.yaml) and global
    (~/.hookwise/config.yaml) paths, deep-merges them with project
    overriding global, interpolates environment variables, validates
    the structure, and resolves handler definitions.

    Usage::

        engine = ConfigEngine()
        config = engine.load_config(project_dir="/path/to/project")
        # config is a HooksConfig dataclass ready for use
    """

    def load_config(self, project_dir: str | Path | None = None) -> HooksConfig:
        """Load, merge, and validate config from project + global paths.

        Resolution order:
        1. Global config: ~/.hookwise/config.yaml
        2. Project config: {project_dir}/hookwise.yaml
        3. Deep merge: project overrides global

        If neither file exists, returns a default (empty) HooksConfig.
        If a file exists but contains malformed YAML, logs an error
        and returns default HooksConfig (fail-open).

        Args:
            project_dir: Path to the project directory containing
                hookwise.yaml. If None, uses the current working directory.

        Returns:
            A validated HooksConfig instance.
        """
        global_raw = self._load_yaml(self._global_config_path())
        project_raw = self._load_yaml(self._project_config_path(project_dir))

        # Merge: project overrides global
        if global_raw is not None and project_raw is not None:
            merged = deep_merge(global_raw, project_raw)
        elif project_raw is not None:
            merged = project_raw
        elif global_raw is not None:
            merged = global_raw
        else:
            # No config files found -- return default
            return HooksConfig()

        # Process recipe includes: recipes provide defaults that
        # the user's config overrides. Loaded between merge and
        # interpolation so env vars in recipe configs also get resolved.
        includes = merged.get("includes", [])
        if isinstance(includes, list) and includes:
            merged = self._process_includes(includes, merged, project_dir)

        # Interpolate environment variables
        merged = interpolate_env_vars(merged)

        # Validate
        result = self.validate_config(merged)
        if not result.valid:
            for err in result.errors:
                logger.warning("Config validation: [%s] %s", err.path, err.message)

        return self._build_config(merged)

    def validate_config(self, raw: dict[str, Any]) -> ValidationResult:
        """Validate config structure and return errors with fix suggestions.

        Checks:
        - All top-level keys are recognized sections
        - Version field is a supported integer
        - Handlers have required fields (type, events)
        - Handler types are valid
        - Event types referenced by handlers are valid

        Args:
            raw: Raw config dict parsed from YAML.

        Returns:
            ValidationResult with valid=True if all checks pass,
            or valid=False with a list of errors.
        """
        errors: list[ValidationError] = []

        # Check for unknown top-level keys
        for key in raw:
            if key not in VALID_SECTIONS:
                errors.append(ValidationError(
                    path=key,
                    message=f"Unknown config section: {key!r}",
                    suggestion=f"Valid sections: {', '.join(sorted(VALID_SECTIONS))}",
                ))

        # Validate version
        version = raw.get("version")
        if version is not None and version != 1:
            errors.append(ValidationError(
                path="version",
                message=f"Unsupported config version: {version}",
                suggestion="Currently only version 1 is supported.",
            ))

        # Validate handlers list
        handlers = raw.get("handlers")
        if handlers is not None:
            if not isinstance(handlers, list):
                errors.append(ValidationError(
                    path="handlers",
                    message="'handlers' must be a list",
                    suggestion="handlers should be a list of handler definitions.",
                ))
            else:
                for i, handler in enumerate(handlers):
                    if not isinstance(handler, dict):
                        errors.append(ValidationError(
                            path=f"handlers[{i}]",
                            message="Handler must be a dict",
                        ))
                        continue

                    h_type = handler.get("type")
                    if h_type is None:
                        errors.append(ValidationError(
                            path=f"handlers[{i}].type",
                            message="Handler is missing required 'type' field",
                            suggestion=f"Add type: one of {', '.join(sorted(VALID_HANDLER_TYPES))}",
                        ))
                    elif h_type not in VALID_HANDLER_TYPES:
                        errors.append(ValidationError(
                            path=f"handlers[{i}].type",
                            message=f"Invalid handler type: {h_type!r}",
                            suggestion=f"Valid types: {', '.join(sorted(VALID_HANDLER_TYPES))}",
                        ))

                    events = handler.get("events")
                    if events is None:
                        errors.append(ValidationError(
                            path=f"handlers[{i}].events",
                            message="Handler is missing required 'events' field",
                            suggestion="Add events: list of event types or '*' for all.",
                        ))

        # Validate guards list
        guards = raw.get("guards")
        if guards is not None and not isinstance(guards, list):
            errors.append(ValidationError(
                path="guards",
                message="'guards' must be a list",
                suggestion="guards should be a list of guard definitions.",
            ))

        # Validate includes list
        includes = raw.get("includes")
        if includes is not None and not isinstance(includes, list):
            errors.append(ValidationError(
                path="includes",
                message="'includes' must be a list",
                suggestion="includes should be a list of recipe file paths.",
            ))

        return ValidationResult(valid=len(errors) == 0, errors=errors)

    def resolve_handlers(self, config: HooksConfig) -> list[ResolvedHandler]:
        """Resolve all handler definitions from a HooksConfig into ResolvedHandlers.

        Processes both the 'handlers' list and 'guards' list from config.
        Guards are treated as handlers with phase='guard'.

        Args:
            config: A validated HooksConfig instance.

        Returns:
            Ordered list of ResolvedHandler objects.
        """
        resolved: list[ResolvedHandler] = []

        # Resolve guards (always phase=guard)
        for i, guard_dict in enumerate(config.guards):
            if not isinstance(guard_dict, dict):
                continue
            # Ensure guard has handler-like structure
            guard_copy = dict(guard_dict)
            guard_copy.setdefault("phase", "guard")
            guard_copy.setdefault("type", "builtin")
            handler = _resolve_handler(guard_copy, i)
            if handler is not None:
                handler.phase = "guard"
                resolved.append(handler)

        # Resolve general handlers
        for i, handler_dict in enumerate(config.handlers):
            if not isinstance(handler_dict, dict):
                continue
            handler = _resolve_handler(handler_dict, len(config.guards) + i)
            if handler is not None:
                resolved.append(handler)

        return resolved

    def get_handlers_for_event(
        self, config: HooksConfig, event_type: str
    ) -> list[ResolvedHandler]:
        """Return handlers that match a given event type, in config order.

        Args:
            config: A validated HooksConfig instance.
            event_type: The event type to filter for.

        Returns:
            List of ResolvedHandler objects that respond to the event,
            ordered by their position in the config file.
        """
        all_handlers = self.resolve_handlers(config)
        return [h for h in all_handlers if event_type in h.events]

    # ------------------------------------------------------------------
    # Private helpers
    # ------------------------------------------------------------------

    def _process_includes(
        self,
        includes: list[str],
        user_config: dict[str, Any],
        project_dir: str | Path | None,
    ) -> dict[str, Any]:
        """Process recipe include directives and merge into config.

        Loads each recipe's hooks.yaml and merges it as a base layer
        under the user's config. The user's config always takes
        precedence over recipe defaults.

        Args:
            includes: List of include directive strings.
            user_config: The user's merged config (global + project).
            project_dir: Project directory for resolving relative paths.

        Returns:
            Merged config dict with recipes as defaults.
        """
        try:
            from hookwise.recipes.loader import load_and_merge_recipes

            proj_path = Path(project_dir) if project_dir else None
            return load_and_merge_recipes(includes, user_config, proj_path)
        except Exception as exc:
            logger.error(
                "Failed to process recipe includes: %s (using config without recipes)",
                exc,
            )
            return user_config

    def _global_config_path(self) -> Path:
        """Return the global config file path."""
        return get_state_dir() / GLOBAL_CONFIG_FILENAME

    def _project_config_path(self, project_dir: str | Path | None) -> Path:
        """Return the project config file path."""
        if project_dir is None:
            project_dir = Path.cwd()
        return Path(project_dir) / PROJECT_CONFIG_FILENAME

    def _load_yaml(self, path: Path) -> dict[str, Any] | None:
        """Load a YAML file, returning None if it doesn't exist.

        Returns None (not an error) when the file is missing.
        Returns None and logs a warning on malformed YAML.

        Args:
            path: Path to the YAML file.

        Returns:
            Parsed dict, or None if the file is missing or malformed.
        """
        if not path.is_file():
            return None
        try:
            content = path.read_text(encoding="utf-8")
            parsed = yaml.safe_load(content)
            if parsed is None:
                # Empty YAML file
                return {}
            if not isinstance(parsed, dict):
                logger.warning(
                    "Config file %s does not contain a YAML mapping, ignoring", path,
                )
                return None
            return parsed
        except yaml.YAMLError as exc:
            logger.warning("Malformed YAML in %s: %s", path, exc)
            return None
        except OSError as exc:
            logger.warning("Could not read config file %s: %s", path, exc)
            return None

    def _build_config(self, raw: dict[str, Any]) -> HooksConfig:
        """Build a HooksConfig from a validated raw dict.

        Unknown keys are silently ignored (already warned during validation).
        Missing keys get their dataclass default values.

        Args:
            raw: Merged and interpolated config dict.

        Returns:
            A populated HooksConfig instance.
        """
        return HooksConfig(
            version=raw.get("version", 1),
            guards=raw.get("guards", []),
            coaching=raw.get("coaching", {}),
            analytics=raw.get("analytics", {}),
            greeting=raw.get("greeting", {}),
            sounds=raw.get("sounds", {}),
            status_line=raw.get("status_line", {}),
            cost_tracking=raw.get("cost_tracking", {}),
            transcript_backup=raw.get("transcript_backup", {}),
            handlers=raw.get("handlers", []),
            settings=raw.get("settings", {}),
            includes=raw.get("includes", []),
        )

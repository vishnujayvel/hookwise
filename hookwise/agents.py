"""Multi-agent observability handler for hookwise.

Tracks subagent lifecycle events to build a delegation tree and
detect file conflicts when multiple subagents modify the same file.

On SubagentStart: logs parent-child relationship, type, and task.
On SubagentStop: logs completion, duration, and modified files.
Detects file conflicts across concurrent subagents.
Generates Mermaid diagrams of the agent delegation tree.

The agent tree is stored in memory (per-process) and persisted
to ``~/.hookwise/state/agent-tree.json`` for cross-event access.
"""

from __future__ import annotations

import logging
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from hookwise.state import atomic_write_json, get_state_dir, safe_read_json

logger = logging.getLogger("hookwise")


# ---------------------------------------------------------------------------
# State file
# ---------------------------------------------------------------------------

AGENT_TREE_FILENAME = "agent-tree.json"


def _get_tree_path() -> Path:
    """Return the path to the agent tree state file."""
    state_dir = get_state_dir()
    return state_dir / "state" / AGENT_TREE_FILENAME


# ---------------------------------------------------------------------------
# Agent tree data management
# ---------------------------------------------------------------------------


def load_agent_tree() -> dict[str, Any]:
    """Load the agent tree from disk.

    Returns:
        Agent tree dict with ``agents`` (dict of agent records)
        and ``edges`` (list of parent-child pairs).
    """
    tree = safe_read_json(_get_tree_path(), default={
        "agents": {},
        "edges": [],
        "file_owners": {},
    })
    tree.setdefault("agents", {})
    tree.setdefault("edges", [])
    tree.setdefault("file_owners", {})
    return tree


def save_agent_tree(tree: dict[str, Any]) -> None:
    """Save the agent tree to disk atomically.

    Args:
        tree: The agent tree dict.
    """
    path = _get_tree_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    atomic_write_json(path, tree)


def record_agent_start(
    tree: dict[str, Any],
    agent_id: str,
    parent_id: str,
    agent_type: str,
    task: str,
    timestamp: str | None = None,
) -> dict[str, Any]:
    """Record a subagent start event.

    Args:
        tree: The agent tree dict (mutated in place).
        agent_id: Unique identifier for the subagent.
        parent_id: ID of the parent agent that spawned this subagent.
        agent_type: Type of subagent (e.g., "Explore", "general-purpose").
        task: Description of the task assigned to the subagent.
        timestamp: ISO 8601 timestamp. Uses current UTC if None.

    Returns:
        The updated agent record.
    """
    if timestamp is None:
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    agent_record = {
        "agent_id": agent_id,
        "parent_id": parent_id,
        "agent_type": agent_type,
        "task": task,
        "started_at": timestamp,
        "stopped_at": None,
        "status": "running",
        "files_modified": [],
    }

    tree["agents"][agent_id] = agent_record

    # Record edge
    edge = [parent_id, agent_id]
    if edge not in tree["edges"]:
        tree["edges"].append(edge)

    return agent_record


def record_agent_stop(
    tree: dict[str, Any],
    agent_id: str,
    status: str = "completed",
    files_modified: list[str] | None = None,
    timestamp: str | None = None,
) -> tuple[dict[str, Any] | None, list[dict[str, str]]]:
    """Record a subagent stop event and detect file conflicts.

    Args:
        tree: The agent tree dict (mutated in place).
        agent_id: The subagent that stopped.
        status: Completion status (e.g., "completed", "failed", "timeout").
        files_modified: List of file paths modified by this subagent.
        timestamp: ISO 8601 timestamp. Uses current UTC if None.

    Returns:
        Tuple of (updated agent record or None, list of conflict dicts).
        Each conflict dict has ``file``, ``agents`` keys.
    """
    if timestamp is None:
        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    agent = tree["agents"].get(agent_id)
    if agent is None:
        logger.warning("SubagentStop for unknown agent: %s", agent_id)
        return None, []

    agent["stopped_at"] = timestamp
    agent["status"] = status
    if files_modified:
        agent["files_modified"] = files_modified

    # Detect file conflicts
    conflicts = detect_file_conflicts(tree, agent_id, files_modified or [])

    # Update file_owners tracking
    for fpath in files_modified or []:
        owners = tree["file_owners"].setdefault(fpath, [])
        if agent_id not in owners:
            owners.append(agent_id)

    return agent, conflicts


def detect_file_conflicts(
    tree: dict[str, Any],
    agent_id: str,
    files_modified: list[str],
) -> list[dict[str, str | list[str]]]:
    """Detect when multiple subagents modify the same file.

    Checks the file_owners map for any files that have already been
    modified by another agent.

    Args:
        tree: The agent tree dict.
        agent_id: The current subagent.
        files_modified: Files this subagent modified.

    Returns:
        List of conflict dicts with ``file`` and ``agents`` keys.
    """
    conflicts: list[dict[str, str | list[str]]] = []
    file_owners = tree.get("file_owners", {})

    for fpath in files_modified:
        existing_owners = file_owners.get(fpath, [])
        other_owners = [o for o in existing_owners if o != agent_id]
        if other_owners:
            conflicts.append({
                "file": fpath,
                "agents": other_owners + [agent_id],
            })

    return conflicts


# ---------------------------------------------------------------------------
# Mermaid diagram generation
# ---------------------------------------------------------------------------


def generate_mermaid_diagram(tree: dict[str, Any]) -> str:
    """Generate a Mermaid diagram of the agent delegation tree.

    Produces a top-down graph (``graph TD``) with nodes labeled by
    agent type and edges showing parent-child relationships.

    Args:
        tree: The agent tree dict with ``agents`` and ``edges``.

    Returns:
        Mermaid diagram string.
    """
    agents = tree.get("agents", {})
    edges = tree.get("edges", [])

    if not agents and not edges:
        return "graph TD\n    A[No agents observed]"

    lines = ["graph TD"]

    # Build node labels
    node_labels: dict[str, str] = {}
    for agent_id, agent in agents.items():
        agent_type = agent.get("agent_type", "unknown")
        # Create a safe node ID (alphanumeric)
        safe_id = "".join(c for c in agent_id if c.isalnum())[:16]
        if not safe_id:
            safe_id = f"agent{hash(agent_id) % 10000}"
        node_labels[agent_id] = safe_id

        # Determine label
        task_snippet = agent.get("task", "")[:40]
        if task_snippet:
            label = f"{agent_type}: {task_snippet}"
        else:
            label = agent_type

        lines.append(f"    {safe_id}[{label}]")

    # Build edges
    for parent_id, child_id in edges:
        parent_node = node_labels.get(parent_id)
        child_node = node_labels.get(child_id)

        # If parent isn't tracked, create a placeholder
        if parent_node is None:
            safe_parent = "".join(c for c in parent_id if c.isalnum())[:16]
            if not safe_parent:
                safe_parent = f"parent{hash(parent_id) % 10000}"
            node_labels[parent_id] = safe_parent
            parent_node = safe_parent
            lines.insert(1, f"    {safe_parent}[Main Agent]")

        if child_node is None:
            continue

        lines.append(f"    {parent_node} --> {child_node}")

    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Builtin handler entry point
# ---------------------------------------------------------------------------


def handle(
    event_type: str,
    payload: dict[str, Any],
    config: Any,
) -> dict[str, Any] | None:
    """Builtin handler entry point for the multi-agent observability handler.

    On SubagentStart: records the parent-child agent relationship.
    On SubagentStop: records completion, detects file conflicts.

    Args:
        event_type: The hook event type.
        payload: The event payload dict from stdin.
        config: The HooksConfig instance.

    Returns:
        None (agent tracking is a pure side effect).
    """
    if event_type not in ("SubagentStart", "SubagentStop"):
        return None

    try:
        tree = load_agent_tree()

        if event_type == "SubagentStart":
            agent_id = payload.get("agent_id", "")
            parent_id = payload.get("parent_id", "main")
            agent_type = payload.get("agent_type", "general-purpose")
            task = payload.get("task", "")

            if not agent_id:
                logger.debug("SubagentStart missing agent_id")
                return None

            record_agent_start(
                tree,
                agent_id=agent_id,
                parent_id=parent_id,
                agent_type=agent_type,
                task=task,
            )

            logger.info(
                "Subagent started: %s (type=%s, parent=%s, task=%s)",
                agent_id, agent_type, parent_id, task[:60],
            )

        elif event_type == "SubagentStop":
            agent_id = payload.get("agent_id", "")
            status = payload.get("status", "completed")
            files_modified = payload.get("files_modified", [])

            if not agent_id:
                logger.debug("SubagentStop missing agent_id")
                return None

            if not isinstance(files_modified, list):
                files_modified = []

            agent, conflicts = record_agent_stop(
                tree,
                agent_id=agent_id,
                status=status,
                files_modified=files_modified,
            )

            if agent:
                # Calculate duration if possible
                duration_str = ""
                started = agent.get("started_at", "")
                stopped = agent.get("stopped_at", "")
                if started and stopped:
                    try:
                        t_start = datetime.fromisoformat(
                            started.replace("Z", "+00:00")
                        )
                        t_stop = datetime.fromisoformat(
                            stopped.replace("Z", "+00:00")
                        )
                        duration_secs = (t_stop - t_start).total_seconds()
                        duration_str = f", duration={duration_secs:.1f}s"
                    except (ValueError, TypeError):
                        pass

                logger.info(
                    "Subagent stopped: %s (status=%s%s, files=%d)",
                    agent_id, status, duration_str, len(files_modified),
                )

            # Warn about file conflicts on stderr
            if conflicts:
                for conflict in conflicts:
                    msg = (
                        f"[hookwise] FILE CONFLICT: {conflict['file']} "
                        f"modified by agents: {', '.join(conflict['agents'])}"
                    )
                    print(msg, file=sys.stderr)
                    logger.warning(msg)

        save_agent_tree(tree)

    except Exception as exc:
        # Fail-open: never let agent tracking crash the hook
        logger.error("Agents handle() failed: %s", exc)

    return None

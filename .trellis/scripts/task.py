#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Task Management Script.

Usage:
    python3 task.py create "<title>" [--slug <name>] [--assignee <dev>] [--priority P0|P1|P2|P3] [--parent <dir>] [--package <pkg>] [--no-start]
    python3 task.py add-context <dir> <file> <path> [reason] # Add jsonl entry
    python3 task.py validate <dir>              # Validate jsonl files
    python3 task.py list-context <dir>          # List jsonl entries
    python3 task.py start <dir>                 # Set active task
    python3 task.py current [--source] [--json] # Show active task
    python3 task.py finish                      # Clear active task
    python3 task.py set-branch <dir> <branch>   # Set git branch
    python3 task.py set-base-branch <dir> <branch>  # Set PR target branch
    python3 task.py set-scope <dir> <scope>     # Set scope for PR title
    python3 task.py set-meta <dir> <key> <value>  # Set a task metadata key
    python3 task.py close <task> [--resolve-blocker <code>] [--json]  # Close completed task
    python3 task.py gc --closed [--before 3d]   # Move expired closed tasks
    python3 task.py restore <task> [--json]     # Restore physical task location
    python3 task.py list [--closed|--all]       # List task lifecycle views
    python3 task.py add-subtask <parent-dir> <child-dir>     # Link child to parent
    python3 task.py remove-subtask <parent-dir> <child-dir>  # Unlink child from parent
"""

from __future__ import annotations

import argparse
import json
import sys

from common.log import Colors, colored
from common.paths import (
    DIR_WORKFLOW,
    DIR_TASKS,
    FILE_TASK_JSON,
    get_repo_root,
    get_developer,
    get_tasks_dir,
    get_current_task,
)
from common.active_task import (
    clear_active_task,
    resolve_active_task,
    resolve_context_key,
    set_active_task,
)
from common.io import read_json, write_json
from common.task_utils import resolve_active_task_reference, run_task_hooks
from common.tasks import (
    children_progress,
    get_all_statuses,
    iter_active_tasks,
    iter_closed_tasks,
    iter_task_records,
    task_closeout_view,
)
from task_lifecycle import cmd_close, cmd_gc, cmd_restore

# Import command handlers from split modules (also re-exports for plan.py compatibility)
from common.task_store import (
    cmd_create,
    cmd_set_branch,
    cmd_set_base_branch,
    cmd_set_scope,
    cmd_set_meta,
    cmd_add_subtask,
    cmd_remove_subtask,
)
from common.task_context import (
    cmd_add_context,
    cmd_validate,
    cmd_list_context,
)


# =============================================================================
# Command: start / finish
# =============================================================================
# BEGIN skill-garden patch task-start-brief-validator v0.6
def _validate_planning_brief(full_path, task_json_path) -> bool:
    """Validate that a planning task has a fresh derived brief before start.

    Args:
        full_path: Absolute task directory path.
        task_json_path: Absolute path to the task metadata file.

    Returns:
        True when start may continue, otherwise False.
    """
    try:
        task_json_exists = task_json_path.is_file()
    except OSError as error:
        print(colored(f"Error: Unable to validate planning task status: {error}", Colors.RED))
        print("Hint: Fix task metadata access before retrying task.py start.")
        return False

    if not task_json_exists:
        return True

    data = read_json(task_json_path)
    if not data:
        print(colored("Error: Unable to read task status for brief validation.", Colors.RED))
        print("Hint: Fix task.json before retrying task.py start.")
        return False
    if data.get("status") != "planning":
        return True

    brief_path = full_path / "brief.md"
    try:
        if not brief_path.is_file():
            print(colored("Error: Planning task brief.md is missing.", Colors.RED))
            print("Hint: Run trellis-task-brief, display the full brief, and wait for user confirmation before retrying task.py start.")
            return False
        brief_mtime = brief_path.stat().st_mtime_ns
        authoritative_paths = [
            full_path / name
            for name in ("prd.md", "design.md", "implement.md")
            if (full_path / name).is_file()
        ]
        stale_sources = [
            path.name
            for path in authoritative_paths
            if path.stat().st_mtime_ns > brief_mtime
        ]
    except OSError as error:
        print(colored(f"Error: Unable to validate planning brief freshness: {error}", Colors.RED))
        print("Hint: Fix task artifact access, then refresh and review brief.md before retrying task.py start.")
        return False

    if stale_sources:
        print(colored(
            f"Error: Planning task brief.md is stale; newer artifacts: {', '.join(stale_sources)}.",
            Colors.RED,
        ))
        print("Hint: Run trellis-task-brief, display the refreshed brief, and wait for user confirmation before retrying task.py start.")
        return False

    return True


def _prepare_start_status(task_json_path):
    """Persist planning -> in_progress before binding the session pointer.

    Args:
        task_json_path: Absolute path to the task metadata file.

    Returns:
        Tuple of original metadata, whether status changed, and success status.
    """
    if not task_json_path.is_file():
        return None, False, True
    data = read_json(task_json_path)
    if not data:
        print(colored("Error: Unable to read task.json before start.", Colors.RED))
        return None, False, False
    if data.get("status") != "planning":
        return None, False, True
    original = dict(data)
    updated = dict(data)
    updated["status"] = "in_progress"
    if not write_json(task_json_path, updated):
        print(colored("Error: Failed to persist task status before start.", Colors.RED))
        return original, False, False
    return original, True, True


def _restore_start_status(task_json_path, original) -> bool:
    """Restore task metadata after session pointer binding fails.

    Args:
        task_json_path: Absolute path to the task metadata file.
        original: Metadata captured before the status transition.

    Returns:
        True when no restore is needed or the original metadata was written.
    """
    return original is None or write_json(task_json_path, original)
# END skill-garden patch task-start-brief-validator v0.6

def cmd_start(args: argparse.Namespace) -> int:
    """Set active task."""
    repo_root = get_repo_root()
    task_input = args.dir

    if not task_input:
        print(colored("Error: task directory or name required", Colors.RED))
        return 1

    # Start 只能绑定共享 active 视图中的任务，closed 任务必须先显式 reopen。
    try:
        full_path = resolve_active_task_reference(task_input, repo_root)
    except ValueError as error:
        print(colored(f"Error: {error}", Colors.RED))
        return 1

    if not full_path.is_dir():
        print(colored(f"Error: Task not found: {task_input}", Colors.RED))
        print("Hint: Use task name (e.g., 'my-task') or full path (e.g., '.trellis/tasks/01-31-my-task')")
        return 1

    # Convert to relative path for storage
    try:
        task_dir = full_path.relative_to(repo_root).as_posix()
    except ValueError:
        task_dir = str(full_path)

    task_json_path = full_path / FILE_TASK_JSON
# BEGIN skill-garden patch task-start-brief-guard v0.6
    if not _validate_planning_brief(full_path, task_json_path):
        return 1
# END skill-garden patch task-start-brief-guard v0.6

# BEGIN skill-garden patch task-start-degraded-write-gate v0.6
    if not resolve_context_key():
        original, status_changed, status_ok = _prepare_start_status(task_json_path)
        if not status_ok:
            return 1
        print(colored(
            "ℹ Session identity not available; active-task pointer not persisted "
            "this session (degraded mode). AI continues based on conversation context.",
            Colors.YELLOW,
        ))
        print(colored(
            "Hint: run inside an AI IDE/session that exposes session identity, "
            "or set TRELLIS_CONTEXT_ID before running task.py start.",
            Colors.YELLOW,
        ))
        if status_changed:
            print(colored("✓ Status: planning → in_progress (degraded)", Colors.GREEN))
        if task_json_path.is_file():
            run_task_hooks("after_start", task_json_path, repo_root)
        return 0
# END skill-garden patch task-start-degraded-write-gate v0.6

# BEGIN skill-garden patch task-start-session-write-gate v0.6
    original, status_changed, status_ok = _prepare_start_status(task_json_path)
    if not status_ok:
        return 1

    active = set_active_task(task_dir, repo_root)
    if not active:
        restored = _restore_start_status(task_json_path, original) if status_changed else True
        print(colored("Error: Failed to set current task", Colors.RED))
        if not restored:
            print(colored(
                "Error: Task status rollback also failed; inspect task.json before retrying.",
                Colors.RED,
            ))
        return 1

    print(colored(f"✓ Current task set to: {task_dir}", Colors.GREEN))
    print(f"Source: {active.source}")
    if status_changed:
        print(colored("✓ Status: planning → in_progress", Colors.GREEN))
    print()
    print(colored("The hook will now inject context from this task's jsonl files.", Colors.BLUE))
    run_task_hooks("after_start", task_json_path, repo_root)
    return 0
# END skill-garden patch task-start-session-write-gate v0.6


# BEGIN skill-garden patch task-finish-clear-result v0.6
def cmd_finish(args: argparse.Namespace) -> int:
    """Clear the active task only when session cleanup succeeds."""
    repo_root = get_repo_root()
    result = clear_active_task(repo_root)
    current = result.task_path

    if not result.cleared:
        print(colored(f"Error: Failed to clear current task: {result.error}", Colors.RED))
        return 1
    if not current:
        print(colored("No current task set", Colors.YELLOW))
        return 0

    task_json_path = repo_root / current / FILE_TASK_JSON
    print(colored(f"✓ Cleared current task (was: {current})", Colors.GREEN))
    print(f"Source: {result.source}")

    if task_json_path.is_file():
        run_task_hooks("after_finish", task_json_path, repo_root)
    return 0
# END skill-garden patch task-finish-clear-result v0.6


# BEGIN skill-garden patch task-current-query-contract v0.6
def cmd_current(args: argparse.Namespace) -> int:
    """Show active task."""
    repo_root = get_repo_root()
    active = resolve_active_task(repo_root)

    if getattr(args, "json", False):
        task_obj = None
        if active.task_path:
            data = read_json(repo_root / active.task_path / FILE_TASK_JSON) or {}
            task_obj = {
                "dir": active.task_path,
                "id": data.get("id") or data.get("name"),
                "title": data.get("title"),
                "status": data.get("status"),
                "parent": data.get("parent"),
                "children": data.get("children", []),
                "branch": data.get("branch"),
                "base_branch": data.get("base_branch"),
            }
        print(json.dumps({
            "current_task": task_obj,
            "source": active.source,
            "stale": active.stale,
        }, ensure_ascii=False))
        return 0

    if args.source:
        print(f"Current task: {active.task_path or '(none)'}")
        print(f"Source: {active.source}")
        if active.stale:
            print("State: stale")
        return 0

    if active.task_path:
        print(active.task_path)
        return 0

    print(colored("No current task set", Colors.YELLOW))
    return 0
# END skill-garden patch task-current-query-contract v0.6


# =============================================================================
# Command: list
# =============================================================================

def _display_status(t, all_statuses: dict) -> str:
    """Return the status label to show for a task in `list` output.

    A parent task's stored status stays "planning" until someone runs
    `task.py start` on the parent directly, even while its children are
    actively being worked — a misleading label for anyone scanning the
    list (#399 item 3). Show "active" instead when at least one child is
    past planning; the stored status.json value is left untouched.
    """
    if t.status == "planning" and t.children:
        child_in_flight = any(
            all_statuses.get(c) not in (None, "planning") for c in t.children
        )
        if child_in_flight:
            return "active"
    return t.status


def cmd_list(args: argparse.Namespace) -> int:
    """List active, closed, or all tasks through shared lifecycle views.

    Args:
        args: List filters and output mode.

    Returns:
        Process exit code.
    """
    repo_root = get_repo_root()
    tasks_dir = get_tasks_dir(repo_root)
    current_task = get_current_task(repo_root)
    developer = get_developer(repo_root)
    filter_mine = args.mine
    filter_status = args.status
    as_json = getattr(args, "json", False)
    if getattr(args, "closed", False):
        view_name = "closed"
        selected = iter_closed_tasks(tasks_dir)
    elif getattr(args, "all", False):
        view_name = "all"
        selected = iter_task_records(tasks_dir)
    else:
        view_name = "active"
        selected = iter_active_tasks(tasks_dir)
    tasks = {task.dir_name: task for task in selected}
    all_statuses = get_all_statuses(tasks_dir)

    if filter_mine and not developer:
        if as_json:
            print(json.dumps({"error": "No developer set"}), file=sys.stderr)
        else:
            print(colored("Error: No developer set. Run init_developer.py first", Colors.RED), file=sys.stderr)
        return 1

    def _included(task) -> bool:
        """Apply assignee and work-status filters to one task."""
        if filter_mine and (task.assignee or "-") != developer:
            return False
        return not filter_status or task.status == filter_status

    if as_json:
        items = []
        for dir_name in sorted(tasks):
            task = tasks[dir_name]
            if not _included(task):
                continue
            closeout = task_closeout_view(task, tasks_dir)
            items.append({
                "dir": task.directory.relative_to(repo_root).as_posix(),
                "id": task.raw.get("id") or dir_name,
                "title": task.title,
                "status": task.status,
                "display_status": _display_status(task, all_statuses),
                "closeout": closeout,
                "priority": task.priority,
                "assignee": task.assignee or None,
                "parent": task.parent,
                "children": list(task.children),
                "package": task.package,
            })
        print(json.dumps({"view": view_name, "tasks": items}, ensure_ascii=False))
        return 0

    labels = {"active": "Active tasks", "closed": "Closed tasks", "all": "All tasks"}
    prefix = "My" if filter_mine else labels[view_name]
    if filter_mine:
        print(colored(f"{prefix} {view_name} tasks (assignee: {developer}):", Colors.BLUE))
    else:
        print(colored(f"{prefix}:", Colors.BLUE))
    print()
    count = 0

    def _print_task(dir_name: str, indent: int = 0) -> None:
        """Print one visible task and its visible children."""
        nonlocal count
        task = tasks[dir_name]
        if not _included(task):
            return
        try:
            relative_path = task.directory.relative_to(repo_root).as_posix()
        except ValueError:
            relative_path = str(task.directory)
        marker = f" {colored('<- current', Colors.GREEN)}" if relative_path == current_task else ""
        progress = children_progress(task.children, all_statuses)
        status_label = _display_status(task, all_statuses)
        package_tag = f" @{task.package}" if task.package else ""
        line_prefix = "  " * indent + "  - "
        if filter_mine:
            print(f"{line_prefix}{dir_name}/ ({status_label}){package_tag}{progress}{marker}")
        else:
            print(
                f"{line_prefix}{dir_name}/ ({status_label}){package_tag}{progress} "
                f"[{colored(task.assignee or '-', Colors.CYAN)}]{marker}"
            )
        count += 1
        for child_name in task.children:
            if child_name in tasks:
                _print_task(child_name, indent + 1)

    for dir_name in sorted(tasks):
        parent = tasks[dir_name].parent
        if not parent or parent not in tasks:
            _print_task(dir_name)
    if count == 0:
        print(f"  (no {view_name} tasks)")
    print()
    print(f"Total: {count} task(s)")
    return 0


# =============================================================================
# Command: list-archive
# =============================================================================

def show_usage() -> None:
    """Show usage help."""
    print("""Task Management Script

Usage:
  python3 task.py create <title>                     Create new task directory
  python3 task.py create <title> --package <pkg>     Create task for a specific package
  python3 task.py create <title> --parent <dir>      Create task as child of parent
  python3 task.py create <title> --no-start          Create without making it active in this session
  python3 task.py add-context <dir> <jsonl> <path> [reason]  Add entry to jsonl
  python3 task.py validate <dir>                     Validate jsonl files
  python3 task.py list-context <dir>                 List jsonl entries
  python3 task.py start <dir>                        Set active task
  python3 task.py current [--source]                 Show active task
  python3 task.py finish                             Clear active task
  python3 task.py set-branch <dir> <branch>          Set git branch
  python3 task.py set-base-branch <dir> <branch>     Set PR target branch
  python3 task.py set-scope <dir> <scope>            Set scope for PR title
  python3 task.py set-meta <dir> <key> <value>       Set/overwrite a task metadata key
  python3 task.py close <task> [--resolve-blocker <code>] [--json]  Close a completed task
  python3 task.py gc --closed [--before 3d]          Move expired closed tasks
  python3 task.py restore <task> [--json]            Restore physical task location
  python3 task.py add-subtask <parent> <child>       Link child task to parent
  python3 task.py remove-subtask <parent> <child>    Unlink child from parent
  python3 task.py list [--closed|--all] [--mine] [--status <status>] [--json]

Monorepo options:
  --package <pkg>      Package name (validated against config.yaml packages)

List options:
  --mine, -m           Show only tasks assigned to current developer
  --status, -s <s>     Filter by work status
  --closed             Show the unified closed view
  --all                Show active and closed task views together
  --json               Output machine-readable JSON

Examples:
  python3 task.py create "Add login feature" --slug add-login
  python3 task.py create "Add login feature" --slug add-login --package cli
  python3 task.py create "Add login feature" --meta linear=ENG-123 --meta epic=auth
  python3 task.py create "Child task" --slug child --parent .trellis/tasks/01-21-parent
  python3 task.py add-context <dir> implement .trellis/spec/cli/backend/auth.md "Auth guidelines"
  python3 task.py set-branch <dir> task/add-login
  python3 task.py start .trellis/tasks/01-21-add-login
  python3 task.py current --source
  python3 task.py finish
  python3 task.py close add-login
  python3 task.py add-subtask parent-task child-task  # Link existing tasks
  python3 task.py remove-subtask parent-task child-task
  python3 task.py list                               # List all active tasks
  python3 task.py list --mine                        # List my tasks only
  python3 task.py list --mine --status in_progress   # List my in-progress tasks
""")


# =============================================================================
# Main Entry
# =============================================================================

def main() -> int:
    """CLI entry point."""
    # Deprecation guard: `init-context` was removed in v0.5.0-beta.12.
    # Detect early so argparse doesn't mask the real reason with a generic
    # "invalid choice" error.
    if len(sys.argv) >= 2 and sys.argv[1] == "init-context":
        print(
            colored(
                "Error: `task.py init-context` was removed in v0.5.0-beta.12.",
                Colors.RED,
            ),
            file=sys.stderr,
        )
        print(
            "implement.jsonl / check.jsonl are now seeded on `task.py create` for",
            file=sys.stderr,
        )
        print(
            "sub-agent-capable platforms and curated by the AI during planning when needed.",
            file=sys.stderr,
        )
        print("See .trellis/workflow.md planning artifact guidance or run:", file=sys.stderr)
        print(
            "  python3 ./.trellis/scripts/get_context.py --mode phase --step 1",
            file=sys.stderr,
        )
        print(
            "Use `task.py add-context <dir> implement|check <path> <reason>` to append entries.",
            file=sys.stderr,
        )
        return 2

    parser = argparse.ArgumentParser(
        description="Task Management Script",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    subparsers = parser.add_subparsers(dest="command", help="Commands")

    # create
    p_create = subparsers.add_parser("create", help="Create new task")
    p_create.add_argument("title", help="Task title")
    p_create.add_argument("--slug", "-s", help="Task slug without the MM-DD date prefix")
    p_create.add_argument("--assignee", "-a", help="Assignee developer")
    p_create.add_argument("--priority", "-p", default="P2", help="Priority (P0-P3)")
    p_create.add_argument("--description", "-d", help="Task description")
    p_create.add_argument("--parent", help="Parent task directory (establishes subtask link)")
    p_create.add_argument("--package", help="Package name for monorepo projects")
    p_create.add_argument(
        "--base-branch",
        help="PR target branch (overrides origin/HEAD detection and the checked-out-branch fallback)",
    )
    p_create.add_argument(
        "--meta",
        action="append",
        help="Task metadata key=value (repeatable)",
    )
    p_create.add_argument(
        "--no-start",
        action="store_true",
        help="Create the task without making it active in this session",
    )

    # add-context
    p_add = subparsers.add_parser("add-context", help="Add context entry")
    p_add.add_argument("dir", help="Task directory")
    p_add.add_argument("file", help="JSONL file (implement|check)")
    p_add.add_argument("path", help="File path to add")
    p_add.add_argument("reason", nargs="?", help="Reason for adding")

    # validate
    p_validate = subparsers.add_parser("validate", help="Validate context files")
    p_validate.add_argument("dir", help="Task directory")

    # list-context
    p_listctx = subparsers.add_parser("list-context", help="List context entries")
    p_listctx.add_argument("dir", help="Task directory")

    # start
    p_start = subparsers.add_parser("start", help="Set active task")
    p_start.add_argument("dir", help="Task directory")

    # current
    p_current = subparsers.add_parser("current", help="Show active task")
    p_current.add_argument("--source", action="store_true",
                           help="Show active task source")
    p_current.add_argument("--json", action="store_true",
                           help="Output machine-readable JSON")

    # finish
    subparsers.add_parser("finish", help="Clear active task")

    # set-branch
    p_branch = subparsers.add_parser("set-branch", help="Set git branch")
    p_branch.add_argument("dir", help="Task directory")
    p_branch.add_argument("branch", help="Branch name")

    # set-base-branch
    p_base = subparsers.add_parser("set-base-branch", help="Set PR target branch")
    p_base.add_argument("dir", help="Task directory")
    p_base.add_argument("base_branch", help="Base branch name (PR target)")

    # set-scope
    p_scope = subparsers.add_parser("set-scope", help="Set scope")
    p_scope.add_argument("dir", help="Task directory")
    p_scope.add_argument("scope", help="Scope name")

    # set-meta
    p_setmeta = subparsers.add_parser("set-meta", help="Set/overwrite a task metadata key")
    p_setmeta.add_argument("dir", help="Task directory")
    p_setmeta.add_argument("key", help="Metadata key")
    p_setmeta.add_argument("value", help="Metadata value")

    # close
    p_close = subparsers.add_parser("close", help="Close a completed task")
    p_close.add_argument("task", help="Task directory or name")
    p_close.add_argument(
        "--resolve-blocker",
        action="append",
        default=[],
        help="Explicitly resolve one persisted semantic blocker code (repeatable)",
    )
    p_close.add_argument("--json", action="store_true", help="Output machine-readable JSON")

    # gc
    p_gc = subparsers.add_parser("gc", help="Move expired closed tasks")
    p_gc.add_argument("--closed", action="store_true", help="Required closed-task selector")
    p_gc.add_argument("--before", default="3d", help="Minimum close age, for example 3d")
    p_gc.add_argument("--dry-run", action="store_true", help="Calculate without moving or committing")
    p_gc.add_argument("--json", action="store_true", help="Output machine-readable JSON")

    # restore
    p_restore = subparsers.add_parser("restore", help="Restore a physically moved task")
    p_restore.add_argument("task", help="Archived task directory or name")
    p_restore.add_argument("--dry-run", action="store_true", help="Calculate without moving or committing")
    p_restore.add_argument("--json", action="store_true", help="Output machine-readable JSON")

    # list
    p_list = subparsers.add_parser("list", help="List tasks")
    p_list.add_argument("--mine", "-m", action="store_true", help="My tasks only")
    p_list.add_argument("--status", "-s", help="Filter by work status")
    p_list_view = p_list.add_mutually_exclusive_group()
    p_list_view.add_argument("--closed", action="store_true", help="Show closed tasks")
    p_list_view.add_argument("--all", action="store_true", help="Show active and closed tasks")
    p_list.add_argument("--json", action="store_true", help="Output machine-readable JSON")

    # add-subtask
    p_addsub = subparsers.add_parser("add-subtask", help="Link child task to parent")
    p_addsub.add_argument("parent_dir", help="Parent task directory")
    p_addsub.add_argument("child_dir", help="Child task directory")

    # remove-subtask
    p_rmsub = subparsers.add_parser("remove-subtask", help="Unlink child task from parent")
    p_rmsub.add_argument("parent_dir", help="Parent task directory")
    p_rmsub.add_argument("child_dir", help="Child task directory")

# BEGIN skill-garden patch task-lifecycle-prune-legacy-closed-view-parser v0.6
# END skill-garden patch task-lifecycle-prune-legacy-closed-view-parser v0.6

    args = parser.parse_args()

    if not args.command:
        show_usage()
        return 1

    commands = {
        "create": cmd_create,
        "add-context": cmd_add_context,
        "validate": cmd_validate,
        "list-context": cmd_list_context,
        "start": cmd_start,
        "current": cmd_current,
        "finish": cmd_finish,
        "set-branch": cmd_set_branch,
        "set-base-branch": cmd_set_base_branch,
        "set-scope": cmd_set_scope,
        "set-meta": cmd_set_meta,
        "close": cmd_close,
        "gc": cmd_gc,
        "restore": cmd_restore,
        "add-subtask": cmd_add_subtask,
        "remove-subtask": cmd_remove_subtask,
        "list": cmd_list,
    }

    if args.command in commands:
        return commands[args.command](args)
    else:
        show_usage()
        return 1


if __name__ == "__main__":
    sys.exit(main())

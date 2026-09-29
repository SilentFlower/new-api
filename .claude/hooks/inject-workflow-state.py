#!/usr/bin/env python3
"""Trellis per-turn breadcrumb hook (UserPromptSubmit / BeforeAgent equivalent).

Runs on every user prompt. Resolves the active task through Trellis'
session-aware active task resolver and emits a short <workflow-state>
block reminding the main AI what task is active and its expected flow.

The emitted ``hookEventName`` field is platform-aware: most hosts expect
``UserPromptSubmit`` (Claude Code naming, also accepted by Cursor / Qoder /
CodeBuddy / Droid / Codex / Copilot wiring), but Gemini CLI 0.40.x renamed
its per-turn event to ``BeforeAgent`` and its schema validator rejects the
legacy name. ``_detect_platform`` picks the right value at runtime.
Breadcrumb text is pulled exclusively from workflow.md
[workflow-state:STATUS] tag blocks — workflow.md is the single source of
truth. There are no fallback dicts in this script: when workflow.md is
missing or a tag is absent, the breadcrumb degrades to a generic
"Refer to workflow.md for current step." line so users see (and fix)
the broken state instead of the hook silently masking it.

Which platforms register this hook is decided by SHARED_HOOKS_BY_PLATFORM
in templates/shared-hooks/index.ts — currently Claude, Codex, Gemini,
Qoder, Copilot, CodeBuddy, Droid, Kiro, Trae and ZCode. That table is the
source of truth; each listed platform's collect<Platform>Templates() pulls
this file into its template map through collectSharedHooks(), and a single
writer puts that map on disk at init time. Kiro wires this via the CLI
custom agent's ``hooks.userPromptSubmit`` and the IDE ``.kiro.hook``
``promptSubmit`` event; its output branch emits a plain-text breadcrumb
(Kiro adds hook stdout directly to the conversation context).

Silent exit 0 cases (no output):
  - No .trellis/ directory found (not a Trellis project)
  - task.json malformed or missing status
"""
from __future__ import annotations

import json
import os
import re
import sys
import queue
import threading
from pathlib import Path

# Force UTF-8 on stdin/stdout/stderr on Windows. Default codepage there is
# cp936 / cp1252 / etc. — non-ASCII content (Chinese task names, prd snippets)
# both in stdin (hook payload from host CLI) and stdout (our emitted blocks)
# raises UnicodeDecodeError / UnicodeEncodeError. Equivalent to `python -X utf8`
# but applied per-stream so we don't depend on host CLI's command wiring.
if sys.platform.startswith("win"):
    import io as _io
    for _stream_name in ("stdin", "stdout", "stderr"):
        _stream = getattr(sys, _stream_name, None)
        if _stream is None:
            continue
        if hasattr(_stream, "reconfigure"):
            try:
                _stream.reconfigure(encoding="utf-8", errors="replace")  # type: ignore[union-attr]
            except Exception:
                pass  # Optional Windows stream setup; keep hook startup non-fatal.
        elif hasattr(_stream, "detach"):
            try:
                setattr(sys, _stream_name, _io.TextIOWrapper(_stream.detach(), encoding="utf-8", errors="replace"))
            except Exception:
                pass  # Optional Windows stream setup; keep hook startup non-fatal.
from typing import Optional


# Bootstrap notice for Codex while the session has no active task. Codex does not
# get the full SessionStart overview; this short reminder points the main session
# at the start skill once and leaves the per-turn state block compact.
CODEX_NO_TASK_BOOTSTRAP_NOTICE = """<trellis-bootstrap>
If you have not already loaded Trellis context this session, read the `trellis-start` skill once.
</trellis-bootstrap>"""
# BEGIN skill-garden patch workflow-state-codex-session-start-guard v0.6


def _codex_has_trellis_session_start(root: Path) -> bool:
    """Return whether the managed Codex SessionStart hook is registered."""
    session_start = root / ".codex" / "hooks" / "session-start.py"
    if not session_start.is_file():
        return False

    hooks_path = root / ".codex" / "hooks.json"
    try:
        config = json.loads(hooks_path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return False
    hooks_config = config.get("hooks")
    if not isinstance(hooks_config, dict):
        return False
    groups = hooks_config.get("SessionStart")
    if not isinstance(groups, list):
        return False
    for group in groups:
        hooks = group.get("hooks") if isinstance(group, dict) else None
        if not isinstance(hooks, list):
            continue
        for hook in hooks:
            if not isinstance(hook, dict):
                continue
            command = hook.get("command")
            if isinstance(command, str) and ".codex/hooks/session-start.py" in command:
                return True
    return False
# END skill-garden patch workflow-state-codex-session-start-guard v0.6


# ---------------------------------------------------------------------------
# CWD-robust Trellis root discovery (fixes hook-path-robustness for this hook)
# ---------------------------------------------------------------------------

# BEGIN skill-garden patch workflow-state-worktree-root-fallback v0.6
def find_trellis_root(start: Path) -> Optional[Path]:
    """Walk up from start without crossing into another Git worktree."""
    cur = start.resolve()
    while cur != cur.parent:
        if (cur / ".trellis").is_dir():
            return cur
        if (cur / ".git").exists() or (cur / ".git").is_symlink():
            return None
        cur = cur.parent
    return None


def emit_worktree_local_trellis_missing(data: dict) -> None:
    """Emit a stable bootstrap diagnostic without loading another branch."""
    message = (
        "<worktree-local-trellis-missing>\n"
        "The current Git worktree has no local .trellis directory. "
        "Run `flower-trellis worktree status --target <worktree>` from an external shell.\n"
        "</worktree-local-trellis-missing>"
    )
    platform = _detect_platform(data)
    if platform == "kiro":
        print(message)
        return
    hook_event_name = "BeforeAgent" if platform == "gemini" else "UserPromptSubmit"
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": hook_event_name,
            "additionalContext": message,
        }
    }))
# END skill-garden patch workflow-state-worktree-root-fallback v0.6


# ---------------------------------------------------------------------------
# Active task discovery
# ---------------------------------------------------------------------------

def _detect_platform(input_data: dict) -> str | None:
    if isinstance(input_data.get("cursor_version"), str):
        return "cursor"
    # CLAUDE_PROJECT_DIR is a compatibility alias that several hosts set
    # alongside their own variable — CodeBuddy, ZCode and Trae all do. It must
    # therefore be checked LAST, or every one of them is detected as claude and
    # the context key becomes `claude_<their-session-id>`. That key does not
    # match the session file `task.py start` wrote under the host's real name,
    # so every turn reports no_task while the pointer exists on disk.
    # Observed on CodeBuddy IDE 4.10.4: session file `codebuddy_ae54840e….json`
    # alongside marker `update-check-claude_ae54840e….marker`, same id.
    env_map = {
        "ZCODE_PROJECT_DIR": "zcode",
        "CURSOR_PROJECT_DIR": "cursor",
        "CODEBUDDY_PROJECT_DIR": "codebuddy",
        "FACTORY_PROJECT_DIR": "droid",
        "GEMINI_PROJECT_DIR": "gemini",
        "QODER_PROJECT_DIR": "qoder",
        "KIRO_PROJECT_DIR": "kiro",
        "COPILOT_PROJECT_DIR": "copilot",
        "TRAE_PROJECT_DIR": "trae",
        # Last: the shared alias, only meaningful once no vendor key matched.
        "CLAUDE_PROJECT_DIR": "claude",
    }
    for env_name, platform in env_map.items():
        if os.environ.get(env_name):
            return platform
    script_parts = set(Path(sys.argv[0]).parts)
    if ".claude" in script_parts:
        return "claude"
    if ".cursor" in script_parts:
        return "cursor"
    if ".codex" in script_parts:
        return "codex"
    if ".gemini" in script_parts:
        return "gemini"
    if ".qoder" in script_parts:
        return "qoder"
    if ".codebuddy" in script_parts:
        return "codebuddy"
    if ".factory" in script_parts:
        return "droid"
    if ".kiro" in script_parts:
        return "kiro"
    if ".trae" in script_parts:
        return "trae"
    if ".zcode" in script_parts:
        return "zcode"
    return None


def _resolve_active_task(root: Path, input_data: dict):
    scripts_dir = root / ".trellis" / "scripts"
    if str(scripts_dir) not in sys.path:
        sys.path.insert(0, str(scripts_dir))
    from common.active_task import resolve_active_task  # type: ignore[import-not-found]

    return resolve_active_task(root, input_data, platform=_detect_platform(input_data))


def get_active_task(root: Path, input_data: dict) -> Optional[tuple[str, str, str]]:
    """Return (task_id, status, source) from the current active task."""
    active = _resolve_active_task(root, input_data)
    if not active.task_path:
        return None

    task_dir = Path(active.task_path)
    if not task_dir.is_absolute():
        task_dir = root / task_dir
    if active.stale:
# BEGIN skill-garden patch workflow-state-stale-task-status v0.6
        return task_dir.name, "missing_task", active.source
# END skill-garden patch workflow-state-stale-task-status v0.6

    task_json = task_dir / "task.json"
    if not task_json.is_file():
        return None
    try:
        data = json.loads(task_json.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return None

    task_id = data.get("id") or task_dir.name
    status = data.get("status", "")
    if not isinstance(status, str) or not status:
        return None
    return task_id, status, active.source
# BEGIN skill-garden patch workflow-state-untracked-helper v0.6


def _get_untracked_work(root: Path, input_data: dict) -> Optional[tuple[str, str, str]]:
    """Return (work_id, stage, summary) for the current session's untracked work."""
    scripts_dir = root / ".trellis" / "scripts"
    if str(scripts_dir) not in sys.path:
        sys.path.insert(0, str(scripts_dir))
    try:
        from untracked_flow import read_untracked_state  # type: ignore[import-not-found]

        result = read_untracked_state(
            root,
            input_data,
            platform=_detect_platform(input_data),
        )
    except Exception:
        return None
    if result.get("status") != "hit":
        return None
    work_id = result.get("workId")
    stage = result.get("stage")
    summary = result.get("summary")
    if not all(isinstance(value, str) and value for value in (work_id, stage, summary)):
        return None
    return work_id, stage, summary
# END skill-garden patch workflow-state-untracked-helper v0.6


# ---------------------------------------------------------------------------
# Breadcrumb loading: parse workflow.md, fall back to hardcoded defaults
# ---------------------------------------------------------------------------

# Supports STATUS values with letters, digits, underscores, hyphens
# (so "in-review" / "blocked-by-team" work alongside "in_progress").
_TAG_RE = re.compile(
    r"\[workflow-state:([A-Za-z0-9_-]+)\]\s*\n(.*?)\n\s*\[/workflow-state:\1\]",
    re.DOTALL,
)

def load_breadcrumbs(root: Path) -> dict[str, str]:
    """Parse workflow.md for [workflow-state:STATUS] blocks.

    Returns {status: body_text}. workflow.md is the single source of
    truth — there are no fallback dicts in this script. Missing tags
    (or a missing/unreadable workflow.md) fall back to a generic line
    in build_breadcrumb so users see the broken state and fix
    workflow.md, rather than the hook silently masking the issue.
    """
    workflow = root / ".trellis" / "workflow.md"
    if not workflow.is_file():
        return {}
    try:
        content = workflow.read_text(encoding="utf-8")
    except OSError:
        return {}

    result: dict[str, str] = {}
    for match in _TAG_RE.finditer(content):
        status = match.group(1)
        body = match.group(2).strip()
        if body:
            result[status] = body
    return result


def _read_trellis_config(root: Path) -> dict:
    """Load .trellis/config.yaml via the bundled trellis_config helper.

    The helper lives in .trellis/scripts/common; the hook lives outside the
    scripts tree, so we extend sys.path before importing.
    """
    scripts_dir = root / ".trellis" / "scripts"
    if str(scripts_dir) not in sys.path:
        sys.path.insert(0, str(scripts_dir))
    try:
        from common.trellis_config import read_trellis_config  # type: ignore[import-not-found]
    except Exception:
        return {}
    try:
        return read_trellis_config(root)
    except Exception:
        return {}


DEFAULT_PROMPT_INJECTION_SKIP_KEYWORD = "no-trellis"


def _resolve_skip_keyword(config: dict) -> str:
    """Read `prompt_injection.skip_keyword` from parsed .trellis/config.yaml.

    Mirrors `common.config.get_prompt_injection_config()`. Defaults to
    "no-trellis"; "" disables the escape hatch entirely. A non-string value
    falls back to the default.
    """
    if isinstance(config, dict):
        section = config.get("prompt_injection")
        if isinstance(section, dict):
            raw = section.get("skip_keyword", DEFAULT_PROMPT_INJECTION_SKIP_KEYWORD)
            if isinstance(raw, str):
                return raw
    return DEFAULT_PROMPT_INJECTION_SKIP_KEYWORD


def prompt_has_skip_keyword(prompt: str, keyword: str) -> bool:
    """Case-insensitive, word-boundary match of `keyword` in `prompt`.

    Hyphen counts as a word char so "no-trellisx" / "xno-trellis" /
    "foo-no-trellis" don't match, but punctuation/whitespace boundaries do.
    Empty keyword never matches (disables the escape hatch).
    """
    if not keyword or not isinstance(prompt, str):
        return False
    pattern = r"(?<![\w-])" + re.escape(keyword) + r"(?![\w-])"
    return re.search(pattern, prompt, re.IGNORECASE) is not None


def _resolve_codex_dispatch_mode(config: dict) -> str:
    """Normalize `codex.dispatch_mode` from .trellis/config.yaml to "auto" or "inline".

    Defaults to `auto`. The legacy `sub-agent` value is an alias for `auto`.
    Any other explicit value (including invalid ones) falls back to `inline`
    without per-turn warnings. Shared by `_codex_mode_banner` (the per-turn
    banner) and `resolve_breadcrumb_key` (the breadcrumb tag key) so the two
    stay in lockstep.
    """
    mode = "auto"
    if isinstance(config, dict):
        codex_cfg = config.get("codex")
        if isinstance(codex_cfg, dict):
            cfg_mode = str(codex_cfg.get("dispatch_mode", mode)).strip().lower()
            if cfg_mode == "inline":
                mode = "inline"
            elif cfg_mode in ("auto", "sub-agent"):
                mode = "auto"
            else:
                mode = "inline"
    return mode


def _codex_mode_banner(config: dict) -> str:
    """Emit a `<codex-mode>` banner for the additionalContext payload.

    Reads `codex.dispatch_mode` from .trellis/config.yaml; defaults to
    `auto`, which dispatches Trellis sub-agents using native Codex context
    injection with a child-side fallback. This does not rely on inherited
    parent transcripts: `fork_turns` remains caller-controlled, and
    fresh-history sub-agents still receive their explicit delegated task and
    inherited session configuration. `inline` is an explicit opt-out; the
    legacy `sub-agent` value is an alias for `auto`. Invalid explicit values
    fall back to `inline` without per-turn warnings. The banner makes the
    active mode explicit to Codex AI per turn, complementing the workflow-state
    body which is per-status. Mode tells AI which dispatch protocol to follow;
    workflow-state tells AI what step it's at.
    """
    mode = _resolve_codex_dispatch_mode(config)
    if mode == "auto":
        meaning = (
            "auto: implement/check work defaults to Trellis sub-agents; native Codex "
            "context injection is preferred and child-side loading is the fallback. "
            "The main session still coordinates, clarifies, updates specs, commits, and finishes."
        )
    else:
        meaning = (
            "inline: the main session implements/checks directly; "
            "do not dispatch implement/check sub-agents."
        )
    return f"<codex-mode>{meaning}</codex-mode>"


def resolve_breadcrumb_key(
    status: str, platform: str | None, config: dict
) -> str:
    """Pick the breadcrumb tag key based on Codex dispatch_mode.

    Codex defaults to ``auto`` and therefore uses the ordinary ``<status>``
    breadcrumb for native SubagentStart dispatch with child-side fallback;
    it does not depend on an inherited parent transcript. ``inline`` selects
    the parallel ``<status>-inline`` tag; ``sub-agent`` remains an alias for
    ``auto``. Invalid explicit values fall back to inline without per-turn
    warnings.

    Non-codex platforms return the plain status unchanged.
    """
    if platform == "codex":
        mode = _resolve_codex_dispatch_mode(config)
        return f"{status}-inline" if mode == "inline" else status
    return status


# BEGIN skill-garden patch workflow-state-breadcrumb-subject v0.6
def build_breadcrumb(
    task_id: Optional[str],
    status: str,
    templates: dict[str, str],
    source: str | None = None,
    breadcrumb_key: str | None = None,
    subject_label: str | None = None,
    subject_summary: str | None = None,
) -> str:
    """Build the <workflow-state>...</workflow-state> block.

    - Known status (tag present in workflow.md) → detailed template body
    - Unknown status (no tag, or workflow.md missing) → generic
      "Refer to workflow.md for current step." line
    - `no_task` pseudo-status (task_id is None) → header omits task info
    """
    lookup_key = breadcrumb_key or status
    body = templates.get(lookup_key)
    if body is None and lookup_key != status:
        body = templates.get(status)
    if body is None:
        body = "Refer to workflow.md for current step."
    if subject_label:
        header = subject_label
    else:
        header = f"Status: {status}" if task_id is None else f"Task: {task_id} ({status})"
    if subject_summary:
        body = f"Summary: {subject_summary}\n{body}"
    return f"<workflow-state>\n{header}\n{body}\n</workflow-state>"
# END skill-garden patch workflow-state-breadcrumb-subject v0.6


# BEGIN skill-garden patch workflow-state-conditional-heartbeat v0.6
import hashlib
import time


CONDITIONAL_WORKFLOW_STATE_PLATFORMS = {"codex", "claude"}
DEFAULT_WORKFLOW_STATE_HEARTBEAT_TURNS = 5
WORKFLOW_STATE_TRACKER_VERSION = 1
WORKFLOW_STATE_REFRESH_ARG = "--trellis-session-start-refresh"


def _resolve_heartbeat_turns(config: dict) -> int:
    """Return the configured unchanged-turn heartbeat interval."""
    raw = DEFAULT_WORKFLOW_STATE_HEARTBEAT_TURNS
    if isinstance(config, dict):
        section = config.get("prompt_injection")
        if isinstance(section, dict):
            raw = section.get("heartbeat_turns", raw)
    if isinstance(raw, bool):
        return DEFAULT_WORKFLOW_STATE_HEARTBEAT_TURNS
    if isinstance(raw, int):
        return raw if raw >= 0 else DEFAULT_WORKFLOW_STATE_HEARTBEAT_TURNS
    if isinstance(raw, str) and re.fullmatch(r"[+-]?\d+", raw.strip()):
        value = int(raw.strip())
        return value if value >= 0 else DEFAULT_WORKFLOW_STATE_HEARTBEAT_TURNS
    return DEFAULT_WORKFLOW_STATE_HEARTBEAT_TURNS


def _resolve_workflow_state_context_key(
    root: Path, input_data: dict, platform: str
) -> str | None:
    """Resolve the host session identity through Trellis' shared helper."""
    scripts_dir = root / ".trellis" / "scripts"
    if str(scripts_dir) not in sys.path:
        sys.path.insert(0, str(scripts_dir))
    try:
        from common.active_task import resolve_context_key  # type: ignore[import-not-found]

        context_key = resolve_context_key(input_data, platform)
    except Exception:
        return None
    return context_key if isinstance(context_key, str) and context_key else None


def _workflow_state_tracker_path(
    root: Path, platform: str, context_key: str
) -> Path:
    """Return the opaque per-platform, per-session tracker path."""
    identity = hashlib.sha256(f"{platform}:{context_key}".encode("utf-8")).hexdigest()
    return root / ".trellis" / ".runtime" / "workflow-state" / f"{identity}.json"


def _read_workflow_state_tracker(path: Path, platform: str) -> dict | None:
    """Read and strictly validate a workflow-state tracker."""
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return None
    if not isinstance(value, dict):
        return None
    expected_keys = {
        "version",
        "platform",
        "fingerprint",
        "unchangedTurns",
        "heartbeatTurns",
        "updatedAt",
    }
    if set(value) != expected_keys:
        return None
    if value.get("version") != WORKFLOW_STATE_TRACKER_VERSION:
        return None
    if value.get("platform") != platform:
        return None
    fingerprint = value.get("fingerprint")
    if not isinstance(fingerprint, str) or re.fullmatch(r"[0-9a-f]{64}", fingerprint) is None:
        return None
    unchanged_turns = value.get("unchangedTurns")
    heartbeat_turns = value.get("heartbeatTurns")
    if isinstance(unchanged_turns, bool) or not isinstance(unchanged_turns, int):
        return None
    if isinstance(heartbeat_turns, bool) or not isinstance(heartbeat_turns, int):
        return None
    if unchanged_turns < 0 or heartbeat_turns < 0:
        return None
    if heartbeat_turns == 0 and unchanged_turns != 0:
        return None
    if heartbeat_turns > 0 and unchanged_turns >= heartbeat_turns:
        return None
    updated_at = value.get("updatedAt")
    if not isinstance(updated_at, str) or not updated_at:
        return None
    try:
        time.strptime(updated_at, "%Y-%m-%dT%H:%M:%SZ")
    except ValueError:
        return None
    return value


def _workflow_state_record(
    platform: str,
    fingerprint: str,
    unchanged_turns: int,
    heartbeat_turns: int,
) -> dict:
    """Build the versioned tracker payload."""
    return {
        "version": WORKFLOW_STATE_TRACKER_VERSION,
        "platform": platform,
        "fingerprint": fingerprint,
        "unchangedTurns": unchanged_turns,
        "heartbeatTurns": heartbeat_turns,
        "updatedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


def _write_workflow_state_tracker(path: Path, value: dict) -> bool:
    """Atomically replace a workflow-state tracker after flushing it."""
    temporary = path.with_name(
        f".{path.name}.{os.getpid()}.{threading.get_ident()}.{time.time_ns()}.tmp"
    )
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with temporary.open("x", encoding="utf-8") as handle:
            handle.write(json.dumps(value, indent=2, ensure_ascii=False) + "\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        return True
    except OSError:
        try:
            temporary.unlink()
        except OSError:
            pass
        return False


def _workflow_state_body(
    templates: dict[str, str], status: str, breadcrumb_key: str
) -> str:
    """Return the exact selected workflow-state body or the standard fallback."""
    body = templates.get(breadcrumb_key)
    if body is None and breadcrumb_key != status:
        body = templates.get(status)
    return body if body is not None else "Refer to workflow.md for current step."


def _workflow_state_action(
    templates: dict[str, str], status: str, breadcrumb_key: str
) -> str:
    """Extract the first visible action line from the selected state body."""
    body = _workflow_state_body(templates, status, breadcrumb_key)
    visible = re.sub(r"<!--.*?-->", "", body, flags=re.DOTALL)
    for raw_line in visible.splitlines():
        line = raw_line.strip()
        if line:
            return line
    return "Refer to workflow.md for current step."


def _build_workflow_state_heartbeat(
    subject: str,
    action: str,
    heartbeat_turns: int,
    subject_summary: str | None = None,
) -> str:
    """Build an actionable reminder without repeating the full state body."""
    lines = ["<workflow-state-heartbeat>", subject]
    if subject_summary:
        lines.append(f"Summary: {subject_summary}")
    lines.extend(
        [
            f"Action: {action}",
            (
                f"State unchanged for {heartbeat_turns} user turns. "
                "Continue following the latest full <workflow-state> block."
            ),
            "</workflow-state-heartbeat>",
        ]
    )
    return "\n".join(lines)


def _conditional_workflow_state_context(
    root: Path,
    input_data: dict,
    platform: str | None,
    config: dict,
    full_context: str,
    heartbeat: str,
    force_refresh: bool,
) -> str | None:
    """Choose a full state, heartbeat, or silent output for the current event."""
    if platform not in CONDITIONAL_WORKFLOW_STATE_PLATFORMS:
        return full_context

    context_key = _resolve_workflow_state_context_key(root, input_data, platform)
    if context_key is None:
        return full_context

    heartbeat_turns = _resolve_heartbeat_turns(config)
    fingerprint = hashlib.sha256(full_context.encode("utf-8")).hexdigest()
    tracker_path = _workflow_state_tracker_path(root, platform, context_key)
    tracker = _read_workflow_state_tracker(tracker_path, platform)

    if force_refresh or tracker is None or tracker["fingerprint"] != fingerprint:
        baseline = _workflow_state_record(platform, fingerprint, 0, heartbeat_turns)
        _write_workflow_state_tracker(tracker_path, baseline)
        return full_context

    if tracker["heartbeatTurns"] != heartbeat_turns:
        baseline = _workflow_state_record(platform, fingerprint, 0, heartbeat_turns)
        if not _write_workflow_state_tracker(tracker_path, baseline):
            return full_context
        return None

    unchanged_turns = 0 if heartbeat_turns == 0 else tracker["unchangedTurns"] + 1
    output = None
    if heartbeat_turns > 0 and unchanged_turns >= heartbeat_turns:
        unchanged_turns = 0
        output = heartbeat
    next_record = _workflow_state_record(
        platform, fingerprint, unchanged_turns, heartbeat_turns
    )
    if not _write_workflow_state_tracker(tracker_path, next_record):
        return full_context
    return output
# END skill-garden patch workflow-state-conditional-heartbeat v0.6
# ---------------------------------------------------------------------------
# Entry
# ---------------------------------------------------------------------------

def _load_hook_input() -> dict:
    """Read hook JSON without trusting host runners to close stdin.

    Kiro IDE `runCommand` and similar hook runners can leave stdin open while
    sending no payload. A plain `json.load(sys.stdin)` then blocks forever.
    Normal hook runners write the complete JSON payload and close stdin, so the
    short daemon read preserves that path while failing closed to `{}` for
    non-piping hosts.
    """
    result_queue: "queue.Queue[str | Exception]" = queue.Queue(maxsize=1)

    def _read() -> None:
        try:
            result_queue.put(sys.stdin.read())
        except Exception as exc:
            result_queue.put(exc)

    reader = threading.Thread(target=_read, daemon=True)
    reader.start()
    try:
        raw = result_queue.get(timeout=0.2)
    except queue.Empty:
        return {}

    if isinstance(raw, Exception):
        return {}
    try:
        data = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


# BEGIN skill-garden patch workflow-state-main-subject-routing v0.6
def main() -> int:
    if os.environ.get("TRELLIS_HOOKS") == "0" or os.environ.get("TRELLIS_DISABLE_HOOKS") == "1":
        return 0

    data = _load_hook_input()
    force_refresh = WORKFLOW_STATE_REFRESH_ARG in sys.argv[1:]

    cwd_str = data.get("cwd") or os.getcwd()
    cwd = Path(cwd_str)

    root = find_trellis_root(cwd)
    if root is None:
        emit_worktree_local_trellis_missing(data)
        return 0

    config = _read_trellis_config(root)
    if not force_refresh and prompt_has_skip_keyword(
        data.get("prompt", ""), _resolve_skip_keyword(config)
    ):
        return 0  # user opted out of the per-turn breadcrumb for this turn

    templates = load_breadcrumbs(root)
    platform = _detect_platform(data)
    if force_refresh:
        script_parts = set(Path(sys.argv[0]).parts)
        if ".codex" in script_parts:
            platform = "codex"
        elif ".claude" in script_parts:
            platform = "claude"
    task = get_active_task(root, data)
    subject_summary = None
    if task is None:
        untracked = _get_untracked_work(root, data)
        if untracked is None:
            # No active task or untracked work — still emit a breadcrumb nudging
            # the AI toward intent routing when the user describes real work.
            status = "no_task"
            breadcrumb_key = resolve_breadcrumb_key(status, platform, config)
            subject = "Status: no_task"
            breadcrumb = build_breadcrumb(
                None, status, templates, breadcrumb_key=breadcrumb_key
            )
        else:
            work_id, stage, subject_summary = untracked
            status = "untracked" if stage == "implement" else f"untracked_{stage}"
            breadcrumb_key = resolve_breadcrumb_key(status, platform, config)
            subject = f"Untracked work: {work_id} ({stage})"
            breadcrumb = build_breadcrumb(
                None,
                status,
                templates,
                breadcrumb_key=breadcrumb_key,
                subject_label=subject,
                subject_summary=subject_summary,
            )
    else:
        task_id, status, source = task
        breadcrumb_key = resolve_breadcrumb_key(status, platform, config)
        subject = f"Task: {task_id} ({status})"
        source_for_breadcrumb = None if platform == "codex" else source
        breadcrumb = build_breadcrumb(
            task_id, status, templates, source_for_breadcrumb, breadcrumb_key=breadcrumb_key
        )

    action = _workflow_state_action(templates, status, breadcrumb_key)
    heartbeat = _build_workflow_state_heartbeat(
        subject,
        action,
        _resolve_heartbeat_turns(config),
        subject_summary=subject_summary,
    )
    if platform == "codex":
        parts: list[str] = []
        if task is None and not _codex_has_trellis_session_start(root):
            parts.append(CODEX_NO_TASK_BOOTSTRAP_NOTICE)
        parts.append(_codex_mode_banner(config))
        parts.append(breadcrumb)
        breadcrumb = "\n\n".join(parts)

    breadcrumb = _conditional_workflow_state_context(
        root,
        data,
        platform,
        config,
        breadcrumb,
        heartbeat,
        force_refresh,
    )
    if breadcrumb is None:
        return 0

    # Kiro (CLI userPromptSubmit / IDE promptSubmit) adds a hook's stdout
    # directly to the conversation context — no JSON envelope. Emit the bare
    # breadcrumb text. Conditionally isolated: all other platforms keep the
    # hookSpecificOutput JSON path below unchanged.
    if platform == "kiro":
        print(breadcrumb)
        return 0

    # Gemini CLI 0.40.x rejects "UserPromptSubmit" — its per-turn event is
    # named "BeforeAgent". Other platforms (Claude/Cursor/Qoder/CodeBuddy/
    # Droid/Codex/Copilot) accept the original Claude-style name.
    hook_event_name = (
        "BeforeAgent" if platform == "gemini" else "UserPromptSubmit"
    )

    output = {
        "hookSpecificOutput": {
            "hookEventName": hook_event_name,
            "additionalContext": breadcrumb,
        }
    }
    print(json.dumps(output))
    return 0
# END skill-garden patch workflow-state-main-subject-routing v0.6


if __name__ == "__main__":
    sys.exit(main())

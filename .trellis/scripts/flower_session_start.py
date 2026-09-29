#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""复用 Trellis 原生 SessionStart，将启动上下文分成三个独立输出。"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path


PARTS = ("state", "rules", "stages")
HOOKS = (".codex/hooks/session-start.py", ".claude/hooks/session-start.py")
MAX_PART_CHARS = 8000
WORKFLOW_BLOCK = re.compile(r"<trellis-workflow>\n(.*?)\n</trellis-workflow>\n*", re.DOTALL)
ASTRA_MODEL = "gpt-6-astra"
SOL_MODEL = "gpt-6-sol"
MODEL_HINT_MAX_BYTES = 2048
WORKFLOW_STATE_REFRESH_ARG = "--trellis-session-start-refresh"
MODEL_WORKFLOW_HINT_BODY = """Applies only while the active model is {model}; it does not apply after switching models. Perform checks internally, without a routine checklist report. Keep ordinary answers brief.
When executing the current task:
- Act autonomously within applicable SKILL and WORKFLOW steps. Do not waive required steps, references, review gates or templates because work seems simple, a check seems redundant, or fewer interruptions are preferred.
- Before a step, review its rules and required references. Reuse material already read in full and unchanged; search matches are not full reads.
- Preserve required heading levels, section order, and conditional sections in specified templates. General brevity or no-heading preferences apply to ordinary prose and do not justify flattening, shortening, or reshaping a specified template.
- Resolve conflicts by instruction hierarchy. Follow the owning workflow's phase-transition and review requirements. Permission to begin planning does not approve the final plan.
- Where the workflow requires review of a displayed plan, a generic request such as "commit and push" starts that workflow; it does not itself confirm a plan produced afterward. Reuse approval of the same plan or an explicit waiver within its scope, including valid auto-loop preauthorization. Otherwise, display the plan and wait for the user's confirmation before acting. Verify the actual user reply; displaying a plan or saying "executing as authorized" is not confirmation.
- Before claiming "read", "checked", or "complete", verify actual tool records and artifacts. Successful reading and compliant execution are separate facts.
- When corrected, review the applicable rules, execution records, and actual result before repairing it. If evidence is missing, state uncertainty. Do not invent causes such as "not read", "forgot", or "file missing", or consult unrelated rules in place of the relevant ones."""
ASTRA_WORKFLOW_HINT = (f'<trellis-astra-workflow-hint model="{ASTRA_MODEL}" version="1">\n'
                       f'{MODEL_WORKFLOW_HINT_BODY.format(model=ASTRA_MODEL)}\n'
                       '</trellis-astra-workflow-hint>')
SOL_WORKFLOW_HINT = (f'<trellis-sol-workflow-hint model="{SOL_MODEL}" version="1">\n'
                     f'{MODEL_WORKFLOW_HINT_BODY.format(model=SOL_MODEL)}\n'
                     '</trellis-sol-workflow-hint>')


def _model_workflow_hint(root: Path, config_key: str, hint: str, label: str) -> str:
    """读取指定模型的项目开关，返回该模型提示或空串。

    @param root: 当前部署项目根目录。
    @param config_key: codex 配置下的模型开关键。
    @param hint: 对应模型的完整提示块。
    @param label: 用于诊断的模型名称。
    @return: 完整提示块；显式关闭时为空串，非法配置或超预算时抛出异常。
    """
    scripts_dir = str(root / ".trellis" / "scripts")
    if scripts_dir not in sys.path:
        sys.path.insert(0, scripts_dir)
    from common.trellis_config import read_trellis_config

    config = read_trellis_config(root)
    codex = config.get("codex", {})
    if not isinstance(codex, dict):
        raise ValueError("codex 配置必须为映射")
    enabled = codex.get(config_key, True)
    # 上游无依赖 YAML 读取器返回字符串，不能把字符串 false 当作真值。
    if isinstance(enabled, str) and enabled.lower() in ("true", "false"):
        enabled = enabled.lower() == "true"
    if not isinstance(enabled, bool):
        raise ValueError(f"codex.{config_key} 必须为 true 或 false")
    if not enabled:
        return ""
    if len(hint.encode("utf-8")) > MODEL_HINT_MAX_BYTES:
        raise ValueError(f"{label} 工作流提示超过 2048 字节预算")
    return hint


def _astra_workflow_hint(root: Path) -> str:
    """读取 Astra 独立开关并返回提示。

    @param root: 当前部署项目根目录。
    @return: Astra 提示块；关闭时为空串。
    """
    return _model_workflow_hint(root, "astra_workflow_hint", ASTRA_WORKFLOW_HINT, "Astra")


def _sol_workflow_hint(root: Path) -> str:
    """读取 Sol 独立开关并返回提示。

    @param root: 当前部署项目根目录。
    @return: Sol 提示块；关闭时为空串。
    """
    return _model_workflow_hint(root, "sol_workflow_hint", SOL_WORKFLOW_HINT, "Sol")


def split_workflow(summary: str) -> dict[str, str]:
    """按完整章节拆分工作流，保持原文和顺序。

    @param summary: 原生 hook 生成的工作流摘要。
    @return: rules 与 stages 的无损分段。
    """
    boundary = re.search(r"^### Planning Artifacts\s*$", summary, re.MULTILINE)
    if boundary is None:
        # 不能猜测新模板的边界，否则可能静默遗漏未知的工作流规则。
        raise ValueError("工作流摘要缺少 Planning Artifacts 分段边界")
    return {"rules": summary[:boundary.start()], "stages": summary[boundary.start():]}


def _load_hook(root: Path, hook: str):
    """加载已部署的原生 hook，避免复制平台状态与会话绑定逻辑。"""
    path = root / hook
    spec = spec_from_file_location("flower_native_session_start", path)
    if spec is None or spec.loader is None:
        raise ValueError(f"无法加载原生 hook：{hook}")
    module = module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _run_native_hook(root: Path, hook: str, hook_input: dict) -> dict | None:
    """在独立解释器中执行原生 hook，保留真实标准流边界。

    @param root: 当前部署项目根目录。
    @param hook: 已验证的原生平台 hook 相对路径。
    @param hook_input: 宿主传入并已规范化 cwd 的事件 JSON。
    @return: 原生标准 SessionStart 输出；原生 hook 跳过时返回 None。
    """
    environment = os.environ.copy()
    environment["PYTHONIOENCODING"] = "utf-8"
    result = subprocess.run(
        [sys.executable, "-X", "utf8", str(root / hook)],
        cwd=root,
        env=environment,
        input=json.dumps(hook_input, ensure_ascii=False).encode("utf-8"),
        capture_output=True,
        check=False,
    )
    stderr = result.stderr.decode("utf-8", errors="replace")
    if stderr:
        # 原生诊断属于真实 Hook 证据，不能因包装器捕获输出而被吞掉。
        sys.stderr.write(stderr)
        sys.stderr.flush()
    if result.returncode != 0:
        raise ValueError(f"原生 hook 退出码 {result.returncode}")
    stdout = result.stdout.decode("utf-8-sig")
    if not stdout.strip():
        return None
    output = json.loads(stdout)
    if not isinstance(output, dict):
        raise ValueError("原生 hook 输出必须为 JSON 对象")
    return output


def _run_workflow_state_refresh(root: Path, hook: str, hook_input: dict) -> str:
    """在独立解释器中刷新当前完整 workflow-state 与会话基线。

    @param root: 当前部署项目根目录。
    @param hook: 已验证的原生平台 SessionStart hook 相对路径。
    @param hook_input: 宿主传入并已规范化 cwd 的事件 JSON。
    @return: workflow-state Hook 返回的完整 additionalContext。
    """
    workflow_hook = hook.replace("session-start.py", "inject-workflow-state.py")
    environment = os.environ.copy()
    environment["PYTHONIOENCODING"] = "utf-8"
    result = subprocess.run(
        [
            sys.executable,
            "-X",
            "utf8",
            str(root / workflow_hook),
            WORKFLOW_STATE_REFRESH_ARG,
        ],
        cwd=root,
        env=environment,
        input=json.dumps(hook_input, ensure_ascii=False).encode("utf-8"),
        capture_output=True,
        check=False,
    )
    stderr = result.stderr.decode("utf-8", errors="replace")
    if stderr:
        sys.stderr.write(stderr)
        sys.stderr.flush()
    if result.returncode != 0:
        raise ValueError(f"workflow-state hook 退出码 {result.returncode}")
    stdout = result.stdout.decode("utf-8-sig")
    if not stdout.strip():
        raise ValueError("workflow-state hook 未返回刷新内容")
    output = json.loads(stdout)
    if not isinstance(output, dict):
        raise ValueError("workflow-state hook 输出必须为 JSON 对象")
    hook_output = output.get("hookSpecificOutput")
    context = hook_output.get("additionalContext") if isinstance(hook_output, dict) else None
    if not isinstance(context, str) or not context:
        raise ValueError("workflow-state hook 缺少 additionalContext")
    return context


def _run_task_maintenance(root: Path) -> str:
    """运行旧任务收敛与三天物理 GC，并返回紧凑诊断。

    @param root: 当前部署项目根目录。
    @return: 有实际动作、延后或错误时的短消息；无动作时为空串。
    """
    script = root / ".trellis/scripts/task_lifecycle.py"
    if not script.is_file():
        return ""
    environment = os.environ.copy()
    environment["PYTHONIOENCODING"] = "utf-8"
    try:
        result = subprocess.run(
            [sys.executable, "-X", "utf8", str(script), "session-start", "--before", "3d", "--json"],
            cwd=root,
            env=environment,
            capture_output=True,
            check=False,
        )
    except OSError as error:
        return f"task maintenance 无法启动：{error}"
    stderr = result.stderr.decode("utf-8", errors="replace").strip()
    if stderr:
        print(stderr, file=sys.stderr)
    stdout = result.stdout.decode("utf-8-sig").strip()
    if result.returncode != 0:
        return f"task maintenance 失败：{stderr or stdout or f'退出码 {result.returncode}'}"
    if not stdout:
        return ""
    try:
        payload = json.loads(stdout)
    except json.JSONDecodeError as error:
        return f"task maintenance 输出损坏：{error}"
    if not isinstance(payload, dict):
        raise ValueError("task maintenance 输出必须为 JSON 对象")
    if payload.get("status") == "busy":
        return ""
    reconciliation = payload.get("reconciliation") if isinstance(payload.get("reconciliation"), dict) else {}
    gc_result = payload.get("gc") if isinstance(payload.get("gc"), dict) else {}
    messages = []
    migrated = reconciliation.get("migrated") if isinstance(reconciliation.get("migrated"), list) else []
    moved = gc_result.get("moved") if isinstance(gc_result.get("moved"), list) else []
    deferred = [
        *(
            reconciliation.get("deferred")
            if isinstance(reconciliation.get("deferred"), list)
            else []
        ),
        *(gc_result.get("deferred") if isinstance(gc_result.get("deferred"), list) else []),
    ]
    if migrated:
        messages.append(f"收敛旧任务 {len(migrated)} 个")
    if moved:
        messages.append(f"物理 GC {len(moved)} 个")
    if deferred:
        reasons = sorted({str(item.get("reason") or "unknown") for item in deferred if isinstance(item, dict)})
        messages.append(f"延后 {len(deferred)} 项（{', '.join(reasons[:4])}）")
    for item in (reconciliation, gc_result):
        if item.get("error"):
            messages.append(str(item["error"]))
    return "task maintenance：" + "；".join(messages) if messages else ""


def render_part(root: Path, hook: str, part: str, hook_input: dict) -> dict | None:
    """生成指定分段，只有 state 执行原生主入口的副作用。

    @param root: 目标项目根目录。
    @param hook: 已验证的原生平台 hook 相对路径。
    @param part: state、rules 或 stages。
    @param hook_input: 宿主传入的事件 JSON。
    @return: 标准 SessionStart 输出；原生 hook 禁用时返回 None。
    """
    if hook not in HOOKS or part not in PARTS:
        raise ValueError("不支持的 SessionStart hook 或分段")
    source = hook_input.get("source")
    if source == "resume" and part != "state":
        return None
    if part == "state":
        maintenance_message = ""
        if source in ("startup", "resume"):
            maintenance_message = _run_task_maintenance(root)
        if source == "resume":
            if not maintenance_message:
                return None
            return {
                "systemMessage": maintenance_message,
                "hookSpecificOutput": {
                    "hookEventName": "SessionStart",
                    "additionalContext": "",
                },
            }
        result = _run_native_hook(root, hook, hook_input)
        if result is None:
            return None
        if maintenance_message:
            result["systemMessage"] = "\n".join(
                filter(None, [result.get("systemMessage"), maintenance_message])
            )
        context = result["hookSpecificOutput"]["additionalContext"]
        if len(WORKFLOW_BLOCK.findall(context)) != 1:
            raise ValueError("原生启动输出必须包含且仅包含一个 trellis-workflow 块")
        context = WORKFLOW_BLOCK.sub("", context)
        # Claude 原生文件同时兼容其他平台；这里仅输出当前宿主的标准通道。
        result.pop("additional_context", None)
        system_lines = str(result.get("systemMessage") or "").splitlines()
        if system_lines and re.fullmatch(r"Trellis context injected \(\d+ chars\)", system_lines[0]):
            # 原计数对应拆分前的全文；仅删除该行，保留 maintenance 与其他原生诊断。
            remaining = "\n".join(system_lines[1:]).strip()
            if remaining:
                result["systemMessage"] = remaining
            else:
                result.pop("systemMessage", None)
        try:
            workflow_state = _run_workflow_state_refresh(root, hook, hook_input)
            separator = "" if not context or context.endswith("\n") else "\n"
            context = f"{context}{separator}{workflow_state}"
        except Exception as error:
            # 条件注入是可恢复优化；失败时保留原生状态，让下一次用户输入完整降级恢复。
            message = f"workflow-state 基线未刷新：{error}"
            result["systemMessage"] = "\n".join(filter(None, [result.get("systemMessage"), message]))
            print(message, file=sys.stderr)
        if hook == HOOKS[0] and source in ("startup", "clear", "compact"):
            model = hook_input.get("model")
            hint_builder = None
            label = ""
            if model == ASTRA_MODEL:
                hint_builder, label = _astra_workflow_hint, "Astra"
            elif model == SOL_MODEL:
                hint_builder, label = _sol_workflow_hint, "Sol"
            if hint_builder is not None:
                try:
                    hint = hint_builder(root)
                    if hint:
                        context = f"{context}\n{hint}"
                except Exception as error:
                    # 提示是可选增强；失败不能吞掉已成功生成的原生启动上下文。
                    message = f"{label} 工作流提示未注入：{error}"
                    result["systemMessage"] = "\n".join(filter(None, [result.get("systemMessage"), message]))
                    print(message, file=sys.stderr)
    else:
        module = _load_hook(root, hook)
        if module.should_skip_injection():
            return None
        builder = module._build_workflow_toc if hook == HOOKS[0] else module._build_workflow_overview
        platform = "codex" if hook == HOOKS[0] else "claude"
        context = split_workflow(builder(root / ".trellis/workflow.md", platform))[part]
        result = {"hookSpecificOutput": {"hookEventName": "SessionStart"}}

    context = f'<trellis-session-part name="{part}">\n{context}\n</trellis-session-part>'
    result["hookSpecificOutput"]["additionalContext"] = context
    if len(context) > MAX_PART_CHARS:
        # 保留正文供宿主落盘补读，不能为了满足预算再次静默截断规则。
        message = (
            f"Trellis {part} 注入为 {len(context)} 字符，超过 {MAX_PART_CHARS} 字符预算；"
            "请检查分段大小，并补读宿主保存的全文及 .trellis/workflow.md。"
        )
        result["systemMessage"] = "\n".join(filter(None, [result.get("systemMessage"), message]))
    return result


def main() -> int:
    """读取事件并输出一个分段，异常以可见诊断交付。

    @return: 成功或已输出诊断时为 0。
    """
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--hook", required=True, choices=HOOKS)
    parser.add_argument("--part", required=True, choices=PARTS)
    args = parser.parse_args()
    if os.environ.get("TRELLIS_HOOKS") == "0" or os.environ.get("TRELLIS_DISABLE_HOOKS") == "1":
        return 0
    if args.hook == HOOKS[0] and os.environ.get("CODEX_NON_INTERACTIVE") == "1":
        return 0
    try:
        hook_input = json.load(sys.stdin)
        if not isinstance(hook_input, dict):
            raise ValueError("hook 输入必须为 JSON 对象")
        # 脚本随项目部署；使用自身位置，避免宿主 cwd 进入子目录时加载另一份 hook。
        root = Path(__file__).resolve().parents[2]
        hook_input = {**hook_input, "cwd": str(root)}
        result = render_part(root, args.hook, args.part, hook_input)
    except Exception as error:
        message = f"Trellis SessionStart {args.part} 注入失败：{error}"
        print(message, file=sys.stderr)
        result = {
            "systemMessage": message,
            "hookSpecificOutput": {
                "hookEventName": "SessionStart",
                "additionalContext": (
                    f"<trellis-injection-error part=\"{args.part}\">\n"
                    "Startup context is incomplete. Read .trellis/workflow.md and run "
                    "python3 ./.trellis/scripts/get_context.py before continuing.\n"
                    "</trellis-injection-error>"
                ),
            },
        }
    if result is not None:
        print(json.dumps(result, ensure_ascii=False), flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())

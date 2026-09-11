#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""复用 Trellis 原生 SessionStart，将启动上下文分成三个独立输出。"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from contextlib import redirect_stdout
from importlib.util import module_from_spec, spec_from_file_location
from io import StringIO
from pathlib import Path


PARTS = ("state", "rules", "stages")
HOOKS = (".codex/hooks/session-start.py", ".claude/hooks/session-start.py")
MAX_PART_CHARS = 8000
WORKFLOW_BLOCK = re.compile(r"<trellis-workflow>\n(.*?)\n</trellis-workflow>\n*", re.DOTALL)
ASTRA_MODEL = "gpt-6-astra"
ASTRA_HINT_MAX_BYTES = 2048
ASTRA_WORKFLOW_HINT = """<trellis-astra-workflow-hint model="gpt-6-astra" version="1">
Applies only while the active model is gpt-6-astra; it does not apply after switching models. Perform checks internally, without a routine checklist report. Keep ordinary answers brief.
When executing the current task:
- Treat required steps, required references, phase boundaries, and output templates in applicable SKILL and WORKFLOW instructions as execution and delivery checks.
- Before a step, review its rules and required references. Reuse material already read in full and unchanged; search matches are not full reads.
- Preserve required heading levels, section order, and conditional sections in specified templates. General brevity or no-heading preferences apply to ordinary prose and do not justify flattening, shortening, or reshaping a specified template.
- Resolve conflicts by instruction hierarchy. Follow the owning workflow's phase-transition and review requirements, checking the scope of prior authorization; do not infer approval of the final plan from permission to begin planning.
- Before claiming "read", "checked", or "complete", verify actual tool records and artifacts. Successful reading and compliant execution are separate facts.
- When corrected, review the applicable rules, execution records, and actual result before repairing it. If evidence is missing, state uncertainty. Do not invent causes such as "not read", "forgot", or "file missing", or consult unrelated rules in place of the relevant ones.
</trellis-astra-workflow-hint>"""


def _astra_workflow_hint(root: Path) -> str:
    """读取项目开关，返回唯一来源的 Astra 提示或空串。

    @param root: 当前部署项目根目录。
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
    enabled = codex.get("astra_workflow_hint", True)
    # 上游无依赖 YAML 读取器返回字符串，不能把字符串 false 当作真值。
    if isinstance(enabled, str) and enabled.lower() in ("true", "false"):
        enabled = enabled.lower() == "true"
    if not isinstance(enabled, bool):
        raise ValueError("codex.astra_workflow_hint 必须为 true 或 false")
    if not enabled:
        return ""
    if len(ASTRA_WORKFLOW_HINT.encode("utf-8")) > ASTRA_HINT_MAX_BYTES:
        raise ValueError("Astra 工作流提示超过 2048 字节预算")
    return ASTRA_WORKFLOW_HINT


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
    module = _load_hook(root, hook)
    if module.should_skip_injection():
        return None
    if part == "state":
        output = StringIO()
        original_stdin = sys.stdin
        try:
            sys.stdin = StringIO(json.dumps(hook_input, ensure_ascii=False))
            with redirect_stdout(output):
                module.main()
        finally:
            sys.stdin = original_stdin
        if not output.getvalue().strip():
            return None
        result = json.loads(output.getvalue())
        context = result["hookSpecificOutput"]["additionalContext"]
        if len(WORKFLOW_BLOCK.findall(context)) != 1:
            raise ValueError("原生启动输出必须包含且仅包含一个 trellis-workflow 块")
        context = WORKFLOW_BLOCK.sub("", context)
        # Claude 原生文件同时兼容其他平台；这里仅输出当前宿主的标准通道。
        result.pop("additional_context", None)
        if re.fullmatch(r"Trellis context injected \(\d+ chars\)", result.get("systemMessage", "")):
            # 原计数对应拆分前的全文；保留其他原生诊断，避免以后吞掉重要提示。
            result.pop("systemMessage")
        if (hook == HOOKS[0] and hook_input.get("model") == ASTRA_MODEL
                and hook_input.get("source") in ("startup", "clear", "compact")):
            try:
                hint = _astra_workflow_hint(root)
                if hint:
                    context = f"{context}\n{hint}"
            except Exception as error:
                # 提示是可选增强；失败不能吞掉已成功生成的原生启动上下文。
                message = f"Astra 工作流提示未注入：{error}"
                result["systemMessage"] = "\n".join(filter(None, [result.get("systemMessage"), message]))
                print(message, file=sys.stderr)
    else:
        builder = module._build_workflow_toc if hook == HOOKS[0] else module._build_workflow_overview
        context = split_workflow(builder(root / ".trellis/workflow.md"))[part]
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
        if hook_input.get("source") == "resume":
            return 0
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

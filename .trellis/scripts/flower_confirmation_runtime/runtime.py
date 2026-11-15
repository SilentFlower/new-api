"""正式确认编排：完整展示、单次领取与执行记账。"""

from __future__ import annotations

import json
import re
import subprocess
import sys
import time
from uuid import uuid4

from .session_log import SessionLog
from .store import ACTIVE, CLAIM_BUDGET, DISPLAY_BUDGET, confirmation_id, message_hash, now_ms, read_json


ACTION_NAMES = {"implement": "按当前完整 Brief 开工", "repair_all_and_recheck": "修复当前全部待处置问题并重检",
                "execute_git_plan": "执行当前已展示的精确 Git 计划"}
CONTRACT_VERSION = 3
POLICY_TIMEOUT = 40
POLICY_OUTPUT_BYTES = 16 * 1024 * 1024


def _frozen_digest(value):
    return message_hash(json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")))


def inspect_confirmation(store, input_path, seconds=None):
    """复用项目内策略进程，隔离 common 模块的项目上下文。

    @param store: 精确会话存储。
    @param input_path: 项目内材料相对路径。
    @param seconds: 可选单次时长。
    @return: 冻结策略及材料快照。
    """
    # 控制文件仅记录 disabled/repair-required；存在时全部保持人工，包含损坏与漂移。
    control = store.project / ".flower/trellis-control.json"
    if control.exists() or control.is_symlink():
        read_json(control)
        raise ValueError("项目 Trellis 已关闭或需修复，保持人工确认")
    command = [sys.executable, "-X", "utf8", str(store.project / ".trellis/scripts/confirmation_policy.py"),
               "--project", str(store.project), "--input", input_path, "--platform", store.platform, "--session", store.session_id]
    if seconds is not None:
        command.extend(("--seconds", str(seconds)))
    try:
        result = subprocess.run(command, cwd=str(store.project), stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=POLICY_TIMEOUT)
    except subprocess.TimeoutExpired as error:
        raise ValueError(f"确认策略检查超时（{POLICY_TIMEOUT} 秒）") from error
    if len(result.stdout) > POLICY_OUTPUT_BYTES:
        raise ValueError("确认策略结构化输出超过 16 MiB 预算")
    try:
        value = json.loads(result.stdout)
    except ValueError:
        raise ValueError("确认策略 helper 不可用或输出无效，保持人工确认")
    if result.returncode:
        raise ValueError(value.get("error") or "确认材料校验失败")
    if (value.get("version") != 1 or value.get("action") not in ACTION_NAMES or not isinstance(value.get("body"), str)
            or type(value.get("seconds")) is not int or not 1 <= value["seconds"] <= 86400):
        raise ValueError("确认策略契约不匹配")
    return value


def render_confirmation(frozen):
    """生成完整展示及本次计时提示。

    @param frozen: 冻结材料和策略。
    @return: 必须完整展示的正文。
    """
    return (frozen["body"] + "\n\n后台确认：本次等待 " + str(frozen["seconds"])
            + " 秒。若期间未提交新消息，将" + ACTION_NAMES[frozen["action"]]
            + "。你可直接输入确认、修改意见、暂停或取消；单次改时会替换旧等待。")


def _normalize_display(text):
    fence = None
    after_heading = False
    lines = []
    for line in text.replace("\r\n", "\n").replace("\r", "\n").split("\n"):
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
        if marker:
            current, rest = marker.groups()
            if fence is None:
                fence = current
            elif current[0] == fence[0] and len(current) >= len(fence) and not rest.strip():
                fence = None
            after_heading = False
        elif fence is None:
            if after_heading and line == "":
                continue
            after_heading = bool(re.match(r"^ {0,3}#{1,6}(?:[ \t]+|$)", line))
        lines.append(line)
    return "\n".join(lines)


def _observe(store, request_id, snapshot, *, observed=None):
    identifiers = snapshot.get("humanEvents", []) + [snapshot.get("humanId")]

    def is_new(identifier, request, state):
        return identifier and identifier not in (state.get("lastHumanMessageId"), request["baselineMessageId"])

    # 等待轮询只用已读状态跳过无变化的观察；新消息必须按锁内最新状态重新判断。
    if observed is not None and not any(is_new(identifier, observed["request"], observed) for identifier in identifiers):
        return observed

    def apply(request, state):
        for identifier in identifiers:
            if not is_new(identifier, request, state):
                continue
            state["lastHumanMessageId"] = identifier
            state["generation"] += 1
            if request["state"] in ACTIVE:
                request.update(state="cancelled", error="user_replied")
    return store.change(request_id, apply)


def _revoke(store, client, request):
    if not request or not request.get("queueId") or client is None:
        return
    try:
        result = client.remove(request["queueId"])
    except Exception as error:
        result = ("failed: " + str(error))[:1000]
    store.change(request["id"], lambda current, state: current.update(revocation=result))


def cancel(store, client=None, request_id=None):
    """显式作废当前请求，尽力撤回自己的队列项。

    @param store: 会话存储。
    @param client: 可用的原生客户端或 None。
    @param request_id: 可选精确请求。
    @return: 有界查询状态。
    """
    def apply(value):
        request = (value or {}).get("request")
        if request and (request_id is None or request["id"] == request_id) and request["state"] in ACTIVE:
            request.update(state="cancelled", error="explicit_cancel")
            value["generation"] += 1
        return value
    value = store.update(apply, create=False)
    request = (value or {}).get("request")
    if request and (request_id is None or request["id"] == request_id) and request["state"] == "cancelled":
        _revoke(store, client, request)
    return status(store)


def prepare(store, client, input_path, seconds=None):
    """在当前活动回合冻结材料并创建新代次。

    @param store: 会话存储。
    @param client: 精确原生客户端。
    @param input_path: 项目内材料路径。
    @param seconds: 可选单次秒数。
    @return: prepared 与完整 display。
    """
    initial = store.read() or {}
    if ((initial.get("request") or {}).get("execution") or {}).get("state") == "started":
        raise ValueError("上一超时动作尚未记录终态，先据实记录结果再准备新确认")
    generation = initial.get("generation", 0)
    frozen = inspect_confirmation(store, input_path, seconds)
    if not frozen["enabled"]:
        return {"prepared": False, "mode": "manual", "reason": frozen.get("reason")}
    observe_prepare = getattr(client, "prepare_snapshot", client.snapshot)
    snapshot = observe_prepare()
    if (snapshot.get("turn") or {}).get("status") != "inProgress":
        raise ValueError("正式确认必须由当前活动回合准备")
    lifetime = ({"windowBinding": client.capture_window(snapshot["turn"]["id"])} if store.platform == "codex"
                else {"runId": confirmation_id(snapshot.get("runId"))})
    current = observe_prepare()
    human_events = snapshot.get("humanEvents", []) + current.get("humanEvents", [])
    if current["humanId"] != snapshot["humanId"] or any(identifier != snapshot["humanId"] for identifier in human_events):
        raise ValueError("准备期间出现新人工输入")
    log_start = "codexLogStart" if store.platform == "codex" else "claudeLogStart"
    if snapshot.get(log_start):
        if (current.get(log_start) != snapshot[log_start]
                or current["humanId"] != current["prepareBaseline"]):
            raise ValueError("准备期间确认回合或人工输入已变化")
        lifetime[log_start] = current[log_start]
    if snapshot.get("logObservation"):
        SessionLog.verify(snapshot["logObservation"], current.get("logObservation"))
        lifetime["logObservation"] = current["logObservation"]
    # 状态只保存整个计划的摘要；领取时用同一材料重算并返回原有完整上下文。
    contract = {"version": CONTRACT_VERSION, "platform": store.platform, "input": input_path, "secondsOverride": seconds,
                "frozenDigest": _frozen_digest(frozen), **lifetime}
    previous = []
    def apply(old):
        native = (old or {}).get("native") or {}
        if ((old or {}).get("generation", 0) != generation
                or lifetime.get("claudeLogStart") and native.get("turn") != current.get("turn")
                or lifetime.get("codexLogStart") and native.get("turnId") != current["turn"]["id"]):
            raise ValueError("准备期间确认代次已变化")
        if old and old.get("request"):
            previous.append(old["request"])
        next_generation = generation + 1
        return {"version": 2, "sessionId": store.session_id, "generation": next_generation,
                "lastHumanMessageId": snapshot["humanId"], **({"native": old["native"]} if old and old.get("native") else {}),
                "request": {"id": str(uuid4()), "generation": next_generation, "baselineMessageId": snapshot["humanId"],
                            "seconds": frozen["seconds"], "message": frozen["action"], "messageSha256": message_hash(frozen["action"]),
                            "originTurnId": snapshot["turn"]["id"], "state": "prepared", "createdAt": now_ms(), "deadline": None,
                            "queueId": None, "wakeTurnId": None, "workerToken": None, "receipt": None, "error": None,
                            "revocation": None, "contract": contract}}
    value = store.update(apply)
    if previous and previous[0].get("queueId"):
        try:
            client.remove(previous[0]["queueId"])
        except Exception:
            pass
    request = value["request"]
    return {"prepared": True, "sessionId": store.session_id, "requestId": request["id"], "seconds": request["seconds"],
            "action": frozen["action"], "display": render_confirmation(frozen)}


def _validate(store, client, request, stage, snapshot=None):
    contract = request.get("contract") or {}
    if contract.get("version") != CONTRACT_VERSION or contract.get("platform") != store.platform:
        raise ValueError("演练请求或未知契约不能获得正式动作许可")
    if store.platform == "codex":
        client.verify_window(contract.get("windowBinding") or {})
    if contract.get("runId"):
        native = (store.read() or {}).get("native") or {}
        if native.get("runId") != contract["runId"] or native.get("ended") or (snapshot or {}).get("runId") != contract["runId"]:
            raise ValueError("确认请求属于已经结束的旧运行")
    if contract.get("logObservation"):
        SessionLog.verify(contract["logObservation"], (snapshot or {}).get("logObservation"))
    current = inspect_confirmation(store, contract["input"], contract.get("secondsOverride"))
    if (not current["enabled"] or _frozen_digest(current) != contract.get("frozenDigest") or current["seconds"] != request["seconds"]
            or current["action"] != request["message"]):
        raise ValueError("确认材料、策略或当前任务已变化")
    if stage == "display" and _normalize_display(render_confirmation(current)) not in _normalize_display(client.assistant_text(request["originTurnId"])):
        raise ValueError("原回合未完整展示冻结材料、时长及默认动作")
    if stage == "claim" or (stage == "start" and (contract.get("claudeLogStart") or contract.get("codexLogStart"))):
        if store.platform == "codex":
            wake = snapshot.get("wake") or {}
            if wake.get("requestId") != request["id"] or wake.get("turnId") != (snapshot.get("turn") or {}).get("id"):
                raise ValueError("Codex 领取未关联到本次原生队列消息")
        else:
            notice = snapshot.get("notification") or {}
            if (notice.get("requestId") != request["id"] or notice.get("taskId") != request["queueId"]
                    or notice.get("promptId") != (snapshot.get("turn") or {}).get("id")):
                raise ValueError("Claude 领取未关联到本次原生任务通知")
    return current


def wait(store, client, request_id, interrupted=lambda: False, notify=lambda value: None):
    """后台观察原回合、人工输入与窗口，完整展示后计时。

    @param store: 会话存储。
    @param client: 精确原生客户端。
    @param request_id: 请求 UUID。
    @param interrupted: 信号中断查询。
    @param notify: JSONL 进度回调。
    @return: 请求实际终态。
    """
    request = (store.read() or {}).get("request") or {}
    if request.get("id") != request_id or (request.get("contract") or {}).get("version") != CONTRACT_VERSION:
        raise ValueError("正式请求不存在，保持人工确认")
    token = str(uuid4())
    def own(current, state):
        if current["state"] == "prepared" and not current.get("workerToken"):
            current["workerToken"] = token
    value = store.change(request_id, own)
    if not value or value["request"].get("workerToken") != token:
        return {"id": request_id, "state": "duplicate_worker"}
    began = time.monotonic()
    timer_deadline = None
    last_request = request
    try:
        if store.platform == "codex":
            client.observe_window(request["contract"]["windowBinding"])
        notify({"event": "prepared", "requestId": request_id, "seconds": request["seconds"]})
        while True:
            value = store.read() or {}
            current = value.get("request") or {}
            if current.get("id") != request_id:
                _revoke(store, client, last_request)
                return {"id": request_id, "state": "replaced"}
            request = current
            last_request = request
            if request["state"] not in ACTIVE:
                if request["state"] != "consumed":
                    _revoke(store, client, request)
                return request
            if interrupted():
                raise ValueError("worker_interrupted")
            snapshot = client.snapshot()
            _observe(store, request_id, snapshot, observed=value)
            if time.monotonic() - began > 2 * 86400:
                raise ValueError("worker_lifetime_exceeded")
            if request["state"] == "prepared":
                if time.monotonic() - began > DISPLAY_BUDGET:
                    raise ValueError("display_completion_unverified")
                turn = snapshot.get("turn") or {}
                if turn.get("id") == request["originTurnId"] and turn.get("status") == "inProgress":
                    time.sleep(0.05)
                    continue
                if turn.get("id") == request["originTurnId"] and turn.get("status") != "completed":
                    raise ValueError("origin_turn_interrupted")
                _validate(store, client, request, "display", snapshot)
                def start_timer(current, state):
                    if current["state"] == "prepared":
                        current.update(state="waiting", deadline=now_ms() + current["seconds"] * 1000)
                updated = store.change(request_id, start_timer)
                timer_deadline = time.monotonic() + request["seconds"]
                if updated and updated["request"]["state"] == "waiting":
                    notify({"event": "waiting", "requestId": request_id, "deadline": updated["request"]["deadline"]})
            elif request["state"] == "waiting" and timer_deadline is not None and time.monotonic() >= timer_deadline:
                _validate(store, client, request, "dispatch", snapshot)
                _observe(store, request_id, client.snapshot())
                dispatch = []
                def begin_dispatch(current, state):
                    if current["state"] == "waiting" and state["generation"] == current["generation"]:
                        current["state"] = "dispatching"
                        dispatch.append(True)
                store.change(request_id, begin_dispatch)
                if dispatch:
                    try:
                        queued = client.add(request_id)
                    except (OSError, ValueError, KeyError) as error:
                        def unknown(current, state):
                            if current["state"] == "dispatching":
                                current.update(state="delivery_unknown", error=str(error)[:1000])
                        store.change(request_id, unknown)
                        raise
                    def record_queue(current, state):
                        current["queueId"] = queued["id"]
                        if current["state"] == "dispatching":
                            current.update(state="queued", claimDeadline=now_ms() + CLAIM_BUDGET * 1000)
                    value = store.change(request_id, record_queue)
                    current = (value or {}).get("request") or {}
                    if current.get("id") != request_id or current["state"] != "queued":
                        _revoke(store, client, {"id": request_id, "queueId": queued["id"]})
                    else:
                        notify({"event": "queued", "requestId": request_id, "queueId": queued["id"]})
                    if client.delivery_on_exit:
                        return current
            elif request["state"] == "queued" and now_ms() > request["claimDeadline"]:
                raise ValueError("claim_expired")
            time.sleep(0.05)
    except Exception as error:
        def fail(current, state):
            if current["state"] in ACTIVE:
                current.update(state="cancelled" if interrupted() or str(error) == "frontend_exited" else "failed", error=str(error)[:1000])
        value = store.change(request_id, fail)
        current = (value or {}).get("request") or {}
        _revoke(store, client, current if current.get("id") == request_id else last_request)
        notify({"event": "stopped", "requestId": request_id, "error": str(error)})
        return current if current.get("id") == request_id else {"id": request_id, "state": "replaced"}


def claim(store, client, request_id):
    """原子领取当前原生提醒关联的唯一许可。

    @param store: 会话存储。
    @param client: 精确原生客户端。
    @param request_id: 请求 UUID。
    @return: claimed=true 时包含固定 action、冻结 context 与回执。
    """
    confirmation_id(request_id)
    value = store.read() or {}
    request = value.get("request") or {}
    denied = lambda reason: {"claimed": False, "requestId": request_id, "reason": reason}
    if request.get("id") != request_id:
        return denied("not_current")
    if request["state"] not in ACTIVE:
        return denied(request["state"])
    try:
        snapshot = client.snapshot()
        _observe(store, request_id, snapshot)
        # 原生 Hook 可以先于 transcript 完整行落盘；只等待该精确通知的来源证据，仍由原子领取裁决。
        notification_deadline = time.monotonic() + 1
        while store.platform == "claude" and snapshot.get("notificationPending") and time.monotonic() < notification_deadline:
            current = (store.read() or {}).get("request") or {}
            if current.get("id") != request_id or current.get("state") not in ACTIVE:
                break
            time.sleep(0.02)
            snapshot = client.snapshot()
            _observe(store, request_id, snapshot)
        deadline = time.monotonic() + 1
        while ((store.read() or {}).get("request") or {}).get("state") == "dispatching" and time.monotonic() < deadline:
            time.sleep(0.02)
        request = (store.read() or {}).get("request") or {}
        frozen = _validate(store, client, request, "claim", snapshot)
        latest = client.snapshot()
        _observe(store, request_id, latest)
        claude_turn = (snapshot.get("turn") or {}).get("id") if request["contract"].get("claudeLogStart") else None
        if claude_turn and (latest.get("turn") or {}).get("id") != claude_turn:
            raise ValueError("Claude 领取期间回合已变化")
        result = denied("not_ready")
        def consume(current, state):
            nonlocal result
            if current["state"] != "queued":
                result = denied(current["state"])
                return
            if (state["generation"] != current["generation"] or state["lastHumanMessageId"] != current["baselineMessageId"]
                    or claude_turn and ((state.get("native") or {}).get("turn") or {}).get("id") != claude_turn
                    or now_ms() > current["claimDeadline"] or message_hash(current["message"]) != current["messageSha256"]):
                current.update(state="failed", error="claim_validation_failed")
                result = denied("claim_validation_failed")
                return
            receipt = {"id": str(uuid4()), "at": now_ms(), "source": "policy_timeout", "materialHash": frozen["materialHash"]}
            current.update(state="consumed", receipt=receipt, wakeTurnId=claude_turn)
            result = {"claimed": True, "requestId": request_id, "source": "policy_timeout", "action": frozen["action"],
                      "receipt": receipt, "context": {**frozen["plan"], "body": frozen["body"], "facts": frozen["facts"],
                                                      "materialHash": frozen["materialHash"], "policyHash": frozen["policyHash"]}}
        store.change(request_id, consume)
        return result
    except Exception as error:
        def fail(current, state):
            if current["state"] in ACTIVE:
                current.update(state="failed", error=str(error)[:1000])
        store.change(request_id, fail)
        raise


def record_result(store, client, request_id, receipt, outcome):
    """核验单次 started 许可并记录实际执行终态。

    @param store: 会话存储。
    @param client: started 时提供的原生客户端。
    @param request_id: 请求 UUID。
    @param receipt: 已消费回执 UUID。
    @param outcome: started/completed/interrupted/needs-verification。
    @return: recorded 及实际执行状态。
    """
    if outcome not in ("started", "completed", "interrupted", "needs-verification"):
        raise ValueError("未知执行结果")
    current = (store.read() or {}).get("request") or {}
    if current.get("id") != request_id or current.get("state") != "consumed" or (current.get("receipt") or {}).get("id") != receipt:
        raise ValueError("执行记账缺少本次已消费回执")
    if outcome == "started":
        snapshot = client.snapshot()
        _observe(store, request_id, snapshot)
        _validate(store, client, current, "start", snapshot)
        snapshot = client.snapshot()
        _observe(store, request_id, snapshot)
        if snapshot["humanId"] != current["baselineMessageId"]:
            raise ValueError("人工输入已变化，不能开始旧默认动作")
    recorded = []
    def apply(request, state):
        if request["state"] != "consumed" or (request.get("receipt") or {}).get("id") != receipt:
            return
        if outcome == "started":
            native = state.get("native") or {}
            run_id = request["contract"].get("runId")
            if (state["generation"] != request["generation"] or state["lastHumanMessageId"] != request["baselineMessageId"]
                    or request.get("wakeTurnId") and (native.get("turn") or {}).get("id") != request["wakeTurnId"]
                    or (run_id and (native.get("runId") != run_id or native.get("ended"))) or request.get("execution")):
                return
        elif (request.get("execution") or {}).get("state") != "started":
            return
        request["execution"] = {"state": outcome, "at": now_ms(), "source": "policy_timeout"}
        recorded.append(True)
    value = store.change(request_id, apply)
    return {"recorded": bool(recorded), "requestId": request_id,
            "execution": ((value or {}).get("request") or {}).get("execution")}


def status(store):
    """读取有界摘要，正文与 Git 材料留在冻结请求中。

    @param store: 精确会话存储。
    @return: 零写入状态摘要。
    """
    request = (store.read() or {}).get("request")
    if not request:
        return {"sessionId": store.session_id, "state": "absent"}
    result = {"sessionId": store.session_id, "requestId": request["id"], "state": request["state"],
              "seconds": request["seconds"], "action": request.get("message") if request.get("message") in ACTION_NAMES else None}
    for key in ("deadline", "receipt", "execution", "error", "revocation"):
        result[key] = request.get(key)
    return result

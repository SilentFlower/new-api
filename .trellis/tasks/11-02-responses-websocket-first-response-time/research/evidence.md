# 首响应漏记排查与复现证据

## 适用基线与证据边界

- 排查日期：2026-11-02
- new-api：`67b342c179808f992202d59c63a4f74faf8aa99f`，`git describe` 为 `v1.0.0-rc.24-354-g67b342c17`
- codex-proxy-rs：`e0b7a841`，用于核对链路另一端的现有计时口径
- 证据包括本地源码追踪和合成事件探针；没有访问真实模型、生产日志或生产数据库
- 用户只确认整体代理顺序，没有确认截图记录的实际传输；“流”不能区分 SSE 与 WS

## 源码链路

| 位置 | 已确认事实 |
| --- | --- |
| `relay/common/relay_info.go:534`、`:541` | 初始化 `isFirstResponse=true`，`FirstResponseTime=StartTime-1s` |
| `relay/common/relay_info.go:854` | `SetFirstResponseTime()` 只在第一次调用时设置当前时间 |
| `controller/responses_websocket.go:582`、`:592` | 普通 WS 将响应交给 `handleResponsesWebSocketUpstreamEvent`，这条分支未调用上述计时方法 |
| `controller/responses_websocket.go:639` | 普通 WS 成功终态使用同一 `turn.info` 调用 `PostTextConsumeQuota` |
| `service/log_info_generate.go:131` | `other.frt = FirstResponseTime.UnixMilli() - StartTime.UnixMilli()`，未登记时得到 -1000 |
| `web/src/features/usage-logs/components/timing-metrics-cell.tsx:73`、`:83` | 只有 `frt > 0` 才显示时长，缺失、零或负数显示 `N/A` |
| `relay/helper/stream_scanner.go:262`、`:273` | SSE 跳过非 data 行、空载荷及 DONE，遇到首个业务载荷后调用 `SetFirstResponseTime()` |
| `controller/responses_compact_passthrough_websocket.go:86`、`:91` | Compact WS 已在事件处理入口登记首响应，可作为相邻实现参考 |

普通 Responses WS 的计时遗漏发生在 new-api；当前证据不要求在 CPR 中补偿，也不要求修改前端显示条件

## 已执行的临时探针

探针源文件保存在 [frt-probe.go.txt](frt-probe.go.txt)，是诊断材料，不属于正式测试集

探针使用生产 `GenRelayInfoResponses` 初始化请求，将请求起点设为当前时间前 2 秒，处理相同的合成 `response.created` 和正文增量，再调用生产 `GenerateTextOtherInfo` 读取日志 `frt`
WS 使用普通事件处理函数，SSE 使用既有 `StreamScannerHandler`；没有 sleep、外部网络或真实凭据

本轮运行结果：

```text
WebSocket：frt=-1000 毫秒
SSE：frt=2000 毫秒
PASS
ok github.com/QuantumNous/new-api/controller 0.026s
```

这证明即使请求起点已在 2 秒前，WS 仍漏记，不能用“响应快到不足 1 毫秒”解释此代码路径
探针通过表示成功复现两条路径的差异，不表示问题已经修复；正式回归应断言首响应已正确登记，并先在旧实现中失败

2026-11-02 启动准备时已再次执行同一探针，结果仍为 WebSocket `frt=-1000` 毫秒、SSE `frt=2000` 毫秒；本次复核未修改产品代码，也不替代正式修复回归

在仓库根目录可复跑：

```bash
python3 - <<'PY'
import json
from pathlib import Path
import subprocess
import tempfile

root = Path.cwd()
task = root / ".trellis/tasks/11-02-responses-websocket-first-response-time"
probe_dir = Path(tempfile.mkdtemp(prefix="new-api-frt-probe-"))
probe = probe_dir / "responses_first_response_probe_test.go"
probe.write_text((task / "research/frt-probe.go.txt").read_text())
overlay = probe_dir / "overlay.json"
overlay.write_text(json.dumps({
    "Replace": {str(root / "controller/responses_first_response_probe_test.go"): str(probe)}
}))
subprocess.run([
    "go", "test", "-mod=readonly", f"-overlay={overlay}",
    "-count=1", "-vet=off", "-run",
    "^TestTemporaryResponsesFirstResponseTiming$", "-v", "./controller",
], check=True)
PY
```

上述诊断命令关闭 vet 以只运行最小探针，不能据此声称 vet、全量测试或完整 WS 代理集成已通过

## 实施定位与验证建议

- 在普通 WS 首个响应的所属入口复用 `SetFirstResponseTime()`，核对每轮状态与首次登记幂等性，避免新增包装层或第二个计时事实
- 保留 Compact 的既有分派和流式业务语义，避免为这次缺陷抽取、搬动或重写 WS 转发循环
- 复用 `controller/responses_websocket_test.go` 的连接、合成上游和计费夹具，为正式回归准备必要的 `ChannelMeta`、请求起点与 billing 状态
- 采用显式时间基线和事件顺序验证首响应、后续帧、连续两轮与没有响应，不依赖固定 sleep
- 先运行新增的失败回归，再实现最小修复；之后执行相关 WS、SSE、Compact 测试和所改包的 vet，记录实际命令与缺口
- 若后续拿到实际日志，仅核对相关记录的传输和 `other.frt`，不保存密钥、令牌、请求正文或未脱敏的响应转储

## CPR 对照

CPR 在 `/root/project/codex-proxy-rs/docs/api.md:2570` 定义：OpenAI Responses 的首字从模型执行会话开始，计至首个非空上游响应载荷；SSE 的空载荷和 DONE 不计入，实际首推理与首正文另行观测
对应实现位于 `backend/crates/providers/openai/src/provider/observation.rs:134` 和 `backend/crates/providers/openai/src/transport/canonical.rs:319`
两层网关各自计时，CPR 的数值不能直接作为 new-api 漏记数据的替代值

## 正式修复与定向回归

- 2026-11-02 用户确认完整 Brief 后启动任务；产品代码仅在普通 `handleResponsesWebSocketUpstreamEvent` 入口补充既有 `SetFirstResponseTime()` 调用，不改写转发循环或 Compact 分派
- 新增 `controller/responses_websocket_timing_test.go`：使用生产 `GenRelayInfoResponses` 验证首次事件及日志、后续帧不覆盖、同一连接上下文的逐轮初始化，并用真实本机 WS 控制帧验证 Ping/Pong 不产生首响应
- 扩展既有普通/Compact/普通三轮真实 WS 回归，显式初始化用户、关闭数据导出、启用 SQLite 消费日志落库，核对每轮 `other.frt` 非负且零用量场景不产生费用

先在旧实现运行以下正式回归，确认失败来自首响应未观测和 `frt=-1000`；真实连接的第一、第三轮普通请求同样记录 `-1000`，中间 Compact 轮已有有效计时：

```bash
go test -mod=readonly ./controller -run '^TestResponsesWebSocketFirstResponseTiming|^TestProxyResponsesWebSocketSupportsOrdinaryCompactAndOrdinaryTurns$' -count=1
```

补充最小修复后，同一命令通过（`ok github.com/QuantumNous/new-api/controller 0.101s`）。随后完整 Check-All 的本地验证结果如下：

| 验证命令 | 实际结果 |
| --- | --- |
| 上述正式定向回归 | 通过；包含旧实现先失败的证据 |
| `go test -mod=readonly ./controller ./relay/helper ./relay/common ./service -count=1` | 四个包全部通过，覆盖普通 WS、SSE scanner、Compact WS 和日志/计费既有回归 |
| `go test -mod=readonly ./relay -run 'ResponsesCompactPassthrough' -count=1` | 通过，补充 HTTP Compact 透传回归 |
| `go test -race -mod=readonly ./controller -run 'ResponsesWebSocket\|ResponsesCompactPassthrough' -count=1` | 通过，无竞态报告 |
| `go vet ./controller ./relay/helper ./relay/common ./service` | 退出码 0，无告警 |
| `go build ./...` | 根模块全包构建通过 |
| `git diff --check` | 通过 |
| `gofmt -l controller/responses_websocket.go controller/responses_websocket_test.go controller/responses_websocket_timing_test.go` | 无输出，三个 Go 文件格式符合要求 |

检查范围限定为当前任务的三个 Go 文件及任务材料，保留开始前已存在的其他任务文档与缓存文件。已核对普通 WS → 每轮 RelayInfo → `GenerateTextOtherInfo` → SQLite 消费日志 → 现有前端 `frt > 0` 展示条件，未变更历史记录或前端零值规则

本地使用合成响应、真实本机 WebSocket 和 SQLite，没有调用真实模型、生产日志、生产数据库或浏览器联调；未运行全仓 `go test ./...`。上线后由部署操作者发起普通 Responses WS 请求并核对新消费记录中的首响应，不用历史截图或 CPR 数值替代本层计时

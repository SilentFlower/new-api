# new-api 预算 v2 界面实机走查（2026-09-13）

## 本地环境（可复现）

- 二进制：`CGO_ENABLED=0 go build -o /tmp/new-api-dev .`（分支 build-bak @ 9ba0882e8）。
- 后端：`/tmp/new-api-dev --port 3999`，SQLite `/tmp/newapi-dev-data/new-api.db`；root 密码 `Passw0rd!123`（通过 `POST /api/setup` 初始化）。
- 前端：`cd web && VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3999 bun run dev` → http://localhost:3001。
- 截图脚本：`/tmp/newapi-dev-data/tour.mjs`（Playwright，`executablePath=/opt/google/chrome/chrome`，`chromiumSandbox:false`），截图在 `/tmp/newapi-dev-data/shots/`。
- 预置数据：渠道 #1「演示渠道-OpenAI」，v2 策略 7 行 + 2 时段（中秋假期 date_range、周末低峰 weekly），池子日/周与 astra 行已设用量，root 在「个人每日」有 $16 提额。
- 注意：`PUT /period-policy` 严格键集合比对，行与时段必须带全量字段（含 `created_at`、`start_at`、`end_at` 等），否则 400 `invalid_channel_period_policy` 且无字段级提示。

## 观察（按截图）

1. 打开「用户限制状态」默认落在「预算用量」页签，只有一个空的「选择预算」下拉，整个 72vh 弹窗其余全空。
2. 「周期策略」页签：顶部 3 段说明 + 服务器时区 + 追踪开关，接着两张全展开的时段卡片（各占 ~230px），预算表要滚动一屏才能看到。
3. 预算表本身可读性尚可（10 列在 1440 宽下勉强不横滚）：名称/作用域/周期/时段/模型/上限/已使用-剩余（进度条）/超限时/操作。
4. 行状态不可辨：「周末池子每日」已停用且时段当前未激活，但显示「已使用 $62.5 / 剩余 $0」+ 满进度条，与「池子每日总额」共用计数（`identityKey` 对 daily/weekly 不含 schedule）。停用/未生效行应弱化并标注「与 X 共用计数」。
5. 「个人每日」行显示上限 $10，但已用 $8.4 / 剩余 $7.6 来自 root 的 $16 提额，同一行内数字自相矛盾；且列头「已使用 / 剩余」实际是「用量最高的用户」。
6. 预算编辑 Sheet：8 个字段 + 一句脚注，「完成」按钮只关闭；无删除、无复制、无校验提示。
7. 预览结果是纯文本行（`名称: $上限 · 生效中 · 生效 · 默认`），「确认保存策略」按钮在预览块内部最底端；需要滚动整页 4~5 屏才能到达。
8. 「预算用量」页签选中一行后只显示一个用户表；池子行显示汇总值 + 「设置用量」。
9. 「个人覆盖」页签：搜索结果卡 + 「并发特批」空态 + 「预算特批」表，两处「临时调高」按钮打开同一个并发+预算混合的 AlertDialog。
10. 未成功截到：提额 AlertDialog、移动端（脚本超时），可按需补。

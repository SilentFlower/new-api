import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const source = await readFile(new URL('./bailian-decision.js', import.meta.url), 'utf8');
const plugin = await import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));

/**
 * 创建共享的原生决策上下文。
 * @param {object} overrides 本用例覆盖字段。
 * @returns {object} 独立请求上下文。
 */
function context(overrides = {}) {
  return {
    model: 'decision-model',
    upstreamModel: 'decision-model',
    baseUrl: 'https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode',
    apiKey: 'synthetic-fixture-key',
    publicTaskId: 'fixture-request',
    requestBody: {
      model: 'decision-model',
      state: { zeta: '订单已付款', alpha: '用户申请退款' },
      questions: {
        category: { type: 'choice', criteria: { refund: '退款', other: '其他' } },
        refund: { type: 'noul', instructions: '是否申请退款' },
        severity: { type: 'score', criteria: ['低', '中', '高'] },
      },
    },
    ...overrides,
  };
}

/**
 * 为上下文创建可观察的厂商成功响应。
 * @param {object} ctx 决策上下文。
 * @returns {object} 成功响应。
 */
function response(ctx) {
  return {
    model: ctx.upstreamModel,
    request_id: 'fixture-upstream',
    answers: Object.fromEntries(Object.entries(ctx.requestBody.questions).map(([id, question]) => [id, { type: question.type }])),
    usage: { input_tokens: 83 },
  };
}

test('三类问题转发至工作空间，保留状态顺序和原请求', () => {
  const ctx = context();
  const before = structuredClone(ctx);
  const intent = plugin.native.decodeDecision({ body: { kind: 'json', value: ctx.requestBody } });
  const request = plugin.buildSubmitRequest({ ...ctx, requestBody: intent.requestBody });
  assert.equal(request.url, ctx.baseUrl + '/v1/systemone');
  assert.equal(request.method, 'POST');
  assert.equal(request.headers.Authorization, 'Bearer synthetic-fixture-key');
  assert.deepEqual(Object.keys(request.body.state), ['zeta', 'alpha']);
  assert.deepEqual(request.body, ctx.requestBody);
  assert.deepEqual(ctx, before);
  assert.equal(request.headers['TypeSafe-Danger-Zone-Images'], undefined);
});

test('可选 v1 后缀与渠道映射保留最终上游模型', () => {
  const ctx = context({ model: 'business-alias', baseUrl: context().baseUrl + '/v1/' });
  ctx.requestBody.model = 'business-alias';
  const request = plugin.buildSubmitRequest(ctx);
  assert.equal(request.url, context().baseUrl + '/v1/systemone');
  assert.equal(request.body.model, 'decision-model');
  assert.equal(ctx.requestBody.model, 'business-alias');
  assert.throws(() => plugin.buildSubmitRequest(context({ upstreamModel: 'jev-latest' })), /仅支持/);
});

test('文本、数组状态及单图和多图字段按百炼原样发送', () => {
  for (const state of ['', '退款', ['退款', { order: 1 }]]) {
    const ctx = context();
    Object.assign(ctx.requestBody, { state, image: { url: 'data:image/png;base64,iVBORw0KGgo=' }, images: [{ url: 'https://example.com/image.png' }] });
    assert.deepEqual(plugin.buildSubmitRequest(ctx).body, ctx.requestBody);
  }
});

test('官方问题数、选项数和评分等级边界均可转发', () => {
  const ctx = context();
  ctx.requestBody.questions = Object.fromEntries(Array.from({ length: 16 }, (_, i) => ['q' + i, { type: 'noul' }]));
  ctx.requestBody.questions.q0 = { type: 'choice', criteria: Object.fromEntries(Array.from({ length: 255 }, (_, i) => ['c' + i, '选项'])) };
  ctx.requestBody.questions.q1 = { type: 'score', criteria: Array.from({ length: 255 }, (_, i) => String(i)) };
  ctx.requestBody.images = Array.from({ length: 16 }, () => ({ url: 'https://example.com/image.png' }));
  assert.deepEqual(plugin.buildSubmitRequest(ctx).body, ctx.requestBody);
});

test('非法原生参数在发送前拒绝', async (t) => {
  const cases = [
    ['空状态', { state: null }],
    ['状态缺失', { state: undefined }],
    ['数字状态', { state: 0 }],
    ['问题为空', { questions: {} }],
    ['超出问题数', { questions: Object.fromEntries(Array.from({ length: 17 }, (_, i) => ['q' + i, { type: 'noul' }])) }],
    ['不支持的问题类型', { questions: { q: { type: 'box' } } }],
    ['空选项', { questions: { q: { type: 'choice', criteria: {} } } }],
    ['选项超限', { questions: { q: { type: 'choice', criteria: Object.fromEntries(Array.from({ length: 256 }, (_, i) => ['c' + i, '选项'])) } } }],
    ['评分等级不足', { questions: { q: { type: 'score', criteria: ['低'] } } }],
    ['评分等级超限', { questions: { q: { type: 'score', criteria: Array(256).fill('等级') } } }],
    ['不合法图像', { image: { url: 'file:///tmp/image.png' } }],
    ['不合法多图类型', { images: {} }],
    ['图像超限', { images: Array(17).fill({ url: 'https://example.com/image.png' }) }],
    ['聊天参数', { temperature: 0 }],
    ['Jev 图片字段', { assets: {} }],
  ];
  for (const [name, fields] of cases) {
    await t.test(name, () => {
      const ctx = context();
      Object.assign(ctx.requestBody, fields);
      assert.throws(() => plugin.buildSubmitRequest(ctx));
    });
  }
  assert.throws(() => plugin.native.decodeDecision({ body: { kind: 'multipart' } }), /JSON/);
});

test('成功响应同步完成并保留百炼答案和请求标识', () => {
  const ctx = context();
  const body = response(ctx);
  const result = plugin.parseSubmitResponse(ctx, { statusCode: 200, body });
  assert.equal(result.taskId, 'fixture-request');
  assert.deepEqual(result.immediate, { status: 'SUCCESS', progress: '100%' });
  assert.deepEqual(plugin.native.renderDecision({}, { data: result.taskData }), body);
  for (const tokens of [0, 83, 268288]) {
    body.usage.input_tokens = tokens;
    assert.deepEqual(plugin.extractUsageOnComplete(ctx, result, body), { input_tokens: tokens });
  }
  assert.deepEqual(plugin.extractUsage(ctx), { input_tokens: 268288 });
  assert.equal(plugin.extractUsage({ ...ctx, usagePurpose: 'billing_ratios' }), null);
});

test('已返回答案的异常用量进入宿主保护路径', () => {
  const ctx = context();
  for (const usage of [undefined, {}, { input_tokens: null }, { input_tokens: -1 }, { input_tokens: 1.5 }, { input_tokens: '83' }, { input_tokens: 268289 }, { input_tokens: Infinity }]) {
    const body = { ...response(ctx), usage };
    const result = plugin.parseSubmitResponse(ctx, { statusCode: 200, body });
    assert.equal(result.immediate.status, 'SUCCESS');
    assert.throws(() => plugin.extractUsageOnComplete(ctx, result, body), /用量缺失或超出/);
  }
});

test('错误响应和不匹配答案拒绝成为成功任务', () => {
  const ctx = context();
  assert.throws(() => plugin.parseSubmitResponse(ctx, { statusCode: 400, body: response(ctx) }), /异常状态/);
  for (const changed of [{ model: 'jev-latest' }, { answers: {} }, { answers: { ...response(ctx).answers, refund: { type: 'score' } } }]) {
    assert.throws(() => plugin.parseSubmitResponse(ctx, { statusCode: 200, body: { ...response(ctx), ...changed } }));
  }
  assert.throws(() => plugin.parseSubmitResponse({ ...ctx, publicTaskId: '' }, { statusCode: 200, body: response(ctx) }), /请求标识/);
  assert.deepEqual(plugin.native.error({}, { code: 'invalid', message: '非法参数' }), { code: 'invalid', message: '非法参数' });
  const error = { code: 'InvalidParameter', message: '上游参数错误', request_id: 'fixture-error' };
  assert.deepEqual(plugin.native.error({}, { message: JSON.stringify(error) }), error);
});

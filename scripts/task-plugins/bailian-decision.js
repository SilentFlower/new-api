// SPDX-License-Identifier: AGPL-3.0-only
// 参考 QuantumNous TypeSafe 1.2.0 的同步任务接口，由 SilentFlower 适配百炼决策协议。
// 原始实现：https://github.com/QuantumNous/new-api-plugins/tree/main/plugins/tasks/typesafe
// 百炼契约：https://help.aliyun.com/zh/model-studio/decision-model-api

const MODEL = 'decision-model';
// 按正式版公布的最大输入预留，完成后用厂商报告的输入 Token 结算。
const MAX_INPUT_TOKENS = 268288;
const REQUEST_FIELDS = ['model', 'state', 'questions', 'image', 'images'];

export const meta = {
  apiVersion: 1,
  key: 'bailian-decision',
  name: { en: 'Bailian Decision', zh: '百炼决策模型' },
  version: '1.0.0',
  icon: 'Bailian.Color',
  author: { name: 'QuantumNous / SilentFlower', url: 'https://github.com/QuantumNous' },
  website: 'https://help.aliyun.com/zh/model-studio/decision-model-api',
  description: {
    en: 'Bailian structured decisions with input token billing',
    zh: '百炼结构化决策，按输入 Token 计费',
  },
  auth: 'api_key',
  models: [MODEL],
  requiredCapabilities: ['json-order@1'],
  fetchMode: 'per_task',
  routes: [{
    method: 'POST',
    path: '/v1/systemone',
    type: 'submit',
    decode: 'decodeDecision',
    render: 'renderDecision',
    retainResult: false,
  }],
  usageSchema: {
    input_tokens: {
      type: 'number',
      unit: 'token',
      description: { en: 'Input token unit price', zh: '输入 Token 单价' },
    },
  },
  usageExamples: [{ label: 'decision-model', facts: { input_tokens: 1000 } }],
};

/**
 * 判断值是否为 JSON 对象。
 * @param {unknown} value 待检查值。
 * @returns {boolean} 是否为非数组对象。
 */
function isObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

/**
 * 核对百炼决策请求及其官方参数边界。
 * @param {object} body 客户端请求。
 * @returns {object} 校验后的原请求。
 */
function validateDecision(body) {
  if (!isObject(body)) throw new Error('决策请求必须是 JSON 对象');
  for (const field of Object.keys(body)) {
    if (!REQUEST_FIELDS.includes(field)) throw new Error('百炼决策接口不支持字段：' + field);
  }
  if (typeof body.model !== 'string' || !body.model.trim()) throw new Error('必须指定决策模型');
  if (body.state === null || (typeof body.state !== 'string' && typeof body.state !== 'object')) {
    throw new Error('state 必须是文本、对象或数组');
  }
  if (!isObject(body.questions)) throw new Error('questions 必须是问题对象');
  const ids = Object.keys(body.questions);
  if (ids.length === 0 || ids.length > 16) throw new Error('一次请求须包含 1 至 16 个问题');
  for (const id of ids) {
    const question = body.questions[id];
    if (!isObject(question) || !['choice', 'noul', 'score'].includes(question.type)) {
      throw new Error('问题类型必须为 choice、noul 或 score');
    }
    if (question.instructions !== undefined && typeof question.instructions !== 'string') {
      throw new Error('问题说明必须是文本');
    }
    if (question.type === 'choice') {
      if (!isObject(question.criteria)) throw new Error('choice 须提供选项对象');
      const count = Object.keys(question.criteria).length;
      if (count < 1 || count > 255) throw new Error('choice 须包含 1 至 255 个选项');
    } else if (question.type === 'score') {
      if (!Array.isArray(question.criteria) || question.criteria.length < 2 || question.criteria.length > 255) {
        throw new Error('score 须包含从低到高的 2 至 255 个等级');
      }
    } else if (question.criteria !== undefined && !isObject(question.criteria)) {
      throw new Error('noul 的 criteria 必须是对象');
    }
  }
  const images = [];
  if (body.image !== undefined) images.push(body.image);
  if (body.images !== undefined) {
    if (!Array.isArray(body.images)) throw new Error('images 必须是图像数组');
    images.push(...body.images);
  }
  if (images.length > 16) throw new Error('一次请求最多包含 16 张图像');
  for (const image of images) {
    if (!isObject(image) || typeof image.url !== 'string' ||
        !/^(https?:\/\/|data:image\/[a-zA-Z0-9.+-]+;base64,).+/.test(image.url)) {
      throw new Error('图像须提供 HTTP 地址或 Base64 图像地址');
    }
  }
  return body;
}

/**
 * 按渠道映射确定本次实际发送给百炼的请求。
 * @param {object} ctx 插件驱动上下文。
 * @returns {object} 独立的上游请求对象。
 */
function decisionRequest(ctx) {
  const body = validateDecision(ctx.requestBody);
  const model = ctx.upstreamModel || ctx.model;
  if (model !== MODEL) throw new Error('百炼决策插件仅支持 decision-model');
  const request = { model, state: body.state, questions: body.questions };
  if (body.image !== undefined) request.image = body.image;
  if (body.images !== undefined) request.images = body.images;
  return request;
}

/**
 * 构造发送至渠道工作空间的 System One 请求。
 * @param {object} ctx 插件驱动上下文。
 * @returns {object} 宿主请求描述符。
 */
export function buildSubmitRequest(ctx) {
  const body = decisionRequest(ctx);
  let baseUrl = ctx.baseUrl.replace(/\/+$/, '');
  if (baseUrl.endsWith('/v1')) baseUrl = baseUrl.slice(0, -3);
  return {
    url: baseUrl + '/v1/systemone',
    method: 'POST',
    headers: {
      Authorization: 'Bearer ' + ctx.apiKey,
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body,
  };
}

/**
 * 将百炼同步响应转换为即时完成任务。
 * @param {object} ctx 插件驱动上下文。
 * @param {object} response 宿主解析的上游响应。
 * @returns {object} 即时成功任务及原响应。
 */
export function parseSubmitResponse(ctx, response) {
  if (response.statusCode < 200 || response.statusCode >= 300) throw new Error('百炼决策接口返回异常状态');
  const body = response.body;
  if (!isObject(body) || body.model !== (ctx.upstreamModel || ctx.model) || !isObject(body.answers)) {
    throw new Error('百炼决策响应缺少匹配的模型或答案');
  }
  for (const id of Object.keys(ctx.requestBody.questions)) {
    const answer = Object.prototype.hasOwnProperty.call(body.answers, id) ? body.answers[id] : null;
    if (!isObject(answer) || answer.type !== ctx.requestBody.questions[id].type) {
      throw new Error('百炼决策响应缺少匹配的问题答案');
    }
  }
  if (!ctx.publicTaskId) throw new Error('缺少网关请求标识');
  // 用量留给完成钩子校验，已交付答案的异常用量由宿主保留预扣并记录诊断。
  return { taskId: ctx.publicTaskId, taskData: body, immediate: { status: 'SUCCESS', progress: '100%' } };
}

/**
 * 为一次同步决策预留官方支持的最大输入用量。
 * @param {object} ctx 插件驱动上下文。
 * @returns {object|null} 预留用量；旧倍率扩展查询返回空值。
 */
export function extractUsage(ctx) {
  decisionRequest(ctx);
  if (ctx.usagePurpose === 'billing_ratios') return null;
  return { input_tokens: MAX_INPUT_TOKENS };
}

/**
 * 使用百炼实际报告的输入用量结算。
 * @param {object} ctx 完成上下文。
 * @param {object} result 完成结果。
 * @param {object} body 百炼原始响应。
 * @returns {object} 已验证的实际输入 Token。
 */
export function extractUsageOnComplete(ctx, result, body) {
  const tokens = body && body.usage && body.usage.input_tokens;
  if (!Number.isInteger(tokens) || tokens < 0 || tokens > MAX_INPUT_TOKENS) {
    throw new Error('百炼输入 Token 用量缺失或超出支持范围');
  }
  return { input_tokens: tokens };
}

/**
 * 标识同步决策协议没有轮询请求。
 * @param {object} ctx 任务上下文。
 * @returns {never} 始终抛出不支持轮询的错误。
 */
export function buildQueryRequest(ctx) {
  throw new Error('百炼决策接口不提供异步任务查询');
}

/**
 * 满足 API v1 的轮询钩子签名。
 * @param {object} ctx 任务上下文。
 * @param {object} response 查询响应。
 * @returns {object} 不支持异步查询的状态。
 */
export function parseTaskResult(ctx, response) {
  return { status: 'UNKNOWN', reason: '百炼决策接口不提供异步任务查询' };
}

export const native = {
  /**
   * 解码百炼原生 JSON 请求。
   * @param {object} ctx 原生请求上下文。
   * @returns {object} 宿主提交意图。
   */
  decodeDecision(ctx) {
    if (!ctx.body || ctx.body.kind !== 'json') throw new Error('百炼决策接口须使用 JSON 请求');
    const body = validateDecision(ctx.body.value);
    return { kind: 'submit', model: body.model, action: 'systemone', requestBody: body };
  },
  /**
   * 返回百炼的原生决策结果。
   * @param {object} ctx 原生呈现上下文。
   * @param {object} task 宿主公开任务视图。
   * @returns {object} 百炼原始响应。
   */
  renderDecision(ctx, task) {
    return task.data;
  },
  /**
   * 呈现宿主已脱敏的调用错误。
   * @param {object} ctx 原生呈现上下文。
   * @param {object} error 宿主错误。
   * @returns {object} 百炼格式的错误对象。
   */
  error(ctx, error) {
    try {
      const body = JSON.parse(error.message);
      if (isObject(body) && typeof body.code === 'string' && typeof body.message === 'string') return body;
    } catch {
      // 宿主校验错误使用普通文本，按同一错误信封呈现。
    }
    return { code: error.code || 'DecisionError', message: error.message };
  },
};

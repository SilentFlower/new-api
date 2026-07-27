export type MessageAuditApiResponse<T> = {
  success: boolean
  message: string
  data?: T
}

/**
 * 校验消息审计管理 API 的统一响应，并返回必需的数据字段。
 *
 * @param response 管理 API 返回的统一响应。
 * @returns 响应中的 data，允许调用方显式使用 null 表示无活动任务。
 */
export function unwrapMessageAuditResponse<T>(
  response: MessageAuditApiResponse<T>
): T {
  if (!response.success) {
    throw new Error(response.message)
  }
  if (response.data === undefined) {
    throw new Error(response.message)
  }
  return response.data
}

/** 用户名、显示名和令牌名允许的最大 Unicode 码点数。 */
export const ACCOUNT_NAME_MAX_CODE_POINTS = 64

/**
 * 按 Unicode 码点检查名称长度，避免 JavaScript 的 UTF-16 长度误拒表情。
 * @param value 待校验的名称。
 * @returns 长度未超过限制时返回 true。
 */
export function isAccountNameWithinLimit(value: string): boolean {
  return [...value].length <= ACCOUNT_NAME_MAX_CODE_POINTS
}

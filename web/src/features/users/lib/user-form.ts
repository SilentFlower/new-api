/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { TFunction } from 'i18next'
import { z } from 'zod'

import {
  type PermissionCatalog,
  type AdminPermissionMatrix,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { quotaUnitsToDollars } from '@/lib/format'
import {
  ACCOUNT_NAME_MAX_CODE_POINTS,
  isAccountNameWithinLimit,
} from '@/lib/name-limit'
import { ROLE } from '@/lib/roles'

import { DEFAULT_GROUP } from '../constants'
import type { UserFormData, User } from '../types'

// ============================================================================
// Form Schema
// ============================================================================

/**
 * 生成用户表单校验规则，并按当前语言展示名称长度错误。
 * @param t 当前语言的翻译函数。
 * @returns 用户表单的 Zod schema。
 */
export function getUserFormSchema(t: TFunction) {
  const nameLengthMessage = t('Name must be between {{min}} and {{max}} characters', {
    min: 1,
    max: ACCOUNT_NAME_MAX_CODE_POINTS,
  })
  return z.object({
    username: z
      .string()
      .min(1, 'Username is required')
      .refine(isAccountNameWithinLimit, nameLengthMessage),
    display_name: z
      .string()
      .refine(isAccountNameWithinLimit, nameLengthMessage)
      .optional(),
    password: z.string().optional(),
    role: z.number().optional(),
    quota_dollars: z.number().min(0).optional(),
    group: z.string().optional(),
    remark: z.string().optional(),
    admin_permissions: z
      .record(z.string(), z.record(z.string(), z.boolean()))
      .optional(),
  })
}

export type UserFormValues = z.infer<ReturnType<typeof getUserFormSchema>>

// ============================================================================
// Form Defaults
// ============================================================================

export const USER_FORM_DEFAULT_VALUES: UserFormValues = {
  username: '',
  display_name: '',
  password: '',
  role: 1, // Default to common user
  quota_dollars: 0,
  group: DEFAULT_GROUP,
  remark: '',
  // Filled against the backend catalog at render time; see UsersMutateDrawer.
  admin_permissions: {},
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: UserFormValues,
  userId?: number,
  catalog?: PermissionCatalog
): UserFormData & { id?: number } {
  const payload: UserFormData & { id?: number } = {
    username: data.username,
    display_name: data.display_name || data.username,
    password: data.password || undefined,
  }

  const role = userId === undefined ? data.role || 1 : (data.role ?? 0)

  // Only send the permission matrix when the target is an admin and the catalog
  // is available; without the catalog we cannot build a full matrix, so we omit
  // the field (the backend then leaves existing permissions untouched).
  if (role >= ROLE.ADMIN && catalog) {
    payload.admin_permissions = normalizeAdminPermissions(
      data.admin_permissions as AdminPermissionMatrix | undefined,
      catalog
    )
  }

  // For create: only send required fields
  if (userId === undefined) {
    payload.role = role
  } else {
    // For update: quota is adjusted atomically via /api/user/manage, not sent here
    payload.group = data.group
    payload.remark = data.remark || undefined
    payload.id = userId
  }

  return payload
}

/**
 * Transform user data to form defaults. The admin permission matrix is passed
 * through as-is (the backend already returns a full matrix); it is filled against
 * the catalog at render time in UsersMutateDrawer.
 */
export function transformUserToFormDefaults(user: User): UserFormValues {
  return {
    username: user.username,
    display_name: user.display_name,
    password: '',
    role: user.role,
    quota_dollars: quotaUnitsToDollars(user.quota),
    group: user.group || DEFAULT_GROUP,
    remark: user.remark || '',
    admin_permissions: user.admin_permissions ?? {},
  }
}

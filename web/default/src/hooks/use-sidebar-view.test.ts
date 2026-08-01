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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { NavGroup } from '@/components/layout/types'
import { ROLE } from '@/lib/roles'

import { filterSidebarNavGroupsByRole } from './use-sidebar-view'

const navGroups: NavGroup[] = [
  {
    id: 'personal',
    title: 'Personal',
    items: [{ title: 'Profile', url: '/profile' }],
  },
  {
    id: 'security-alerts',
    title: 'Security Alerts',
    items: [
      {
        title: 'Key Leak Detection',
        url: '/security-alerts/token-leaks',
        requiredRole: ROLE.SUPER_ADMIN,
      },
    ],
  },
]

describe('filterSidebarNavGroupsByRole', () => {
  test('普通用户看不到过滤后为空的安全告警分组', () => {
    const result = filterSidebarNavGroupsByRole(navGroups, ROLE.USER)

    assert.deepEqual(
      result.map((group) => group.id),
      ['personal']
    )
  })

  test('root 用户保留安全告警入口', () => {
    const result = filterSidebarNavGroupsByRole(navGroups, ROLE.SUPER_ADMIN)

    assert.deepEqual(
      result.map((group) => group.id),
      ['personal', 'security-alerts']
    )
  })
})

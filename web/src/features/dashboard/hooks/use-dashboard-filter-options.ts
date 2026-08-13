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
import { useQuery } from '@tanstack/react-query'

import type { Option } from '@/components/multi-select'
import {
  getDashboardGroups,
  getDashboardTokenOptions,
} from '@/features/dashboard/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

export function useDashboardFilterOptions(enabled = true) {
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const isAdmin = Boolean(userRole && userRole >= ROLE.ADMIN)

  const query = useQuery({
    queryKey: ['dashboard', 'filter-options', isAdmin],
    queryFn: async () => {
      const [groups, tokenOptions] = await Promise.all([
        isAdmin ? getDashboardGroups() : Promise.resolve([]),
        getDashboardTokenOptions(isAdmin),
      ])
      const groupOptions: Option[] = groups.map((group) => ({
        value: group,
        label: group,
      }))
      return { groupOptions, tokenOptions }
    },
    enabled,
    staleTime: 60_000,
  })

  return {
    isAdmin,
    isLoading: query.isLoading || query.isFetching,
    groupOptions: query.data?.groupOptions ?? [],
    tokenOptions: query.data?.tokenOptions ?? [],
  }
}

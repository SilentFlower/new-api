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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'

import type { ChannelBudgetStatus } from '../../../lib/channel-budget-status'

/** @param props 预算行状态。 @returns 带语义配色的状态徽标。 */
export function BudgetStatusBadge(props: { status: ChannelBudgetStatus }) {
  const { t } = useTranslation()
  const variants = {
    active: { variant: 'secondary', label: t('Active now') },
    near: { variant: 'warning', label: t('Near limit') },
    exceeded: { variant: 'destructive', label: t('Exceeded') },
    pending: { variant: 'outline', label: t('Schedule inactive') },
    off: { variant: 'ghost', label: t('Disabled') },
  } as const
  const config = variants[props.status]
  return (
    <Badge variant={config.variant} data-status={props.status}>
      {config.label}
    </Badge>
  )
}

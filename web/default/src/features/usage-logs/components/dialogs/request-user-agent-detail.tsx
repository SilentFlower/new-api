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

interface RequestUserAgentDetailProps {
  isAdmin: boolean
  userAgent?: string
}

/**
 * RequestUserAgentDetail 在管理员日志详情中展示原始入站 User-Agent。
 * @param props 管理员权限与待展示的 User-Agent。
 * @returns 有有效管理员 UA 时返回详情行，否则不渲染内容。
 */
export function RequestUserAgentDetail(props: RequestUserAgentDetailProps) {
  const { t } = useTranslation()

  if (!props.isAdmin || !props.userAgent) {
    return null
  }

  return (
    <div className='grid min-w-0 grid-cols-[5.25rem_minmax(0,1fr)] gap-2 text-sm sm:grid-cols-[7rem_minmax(0,1fr)] sm:gap-3'>
      <span className='text-muted-foreground min-w-0 text-xs'>
        {t('User Agent')}
      </span>
      <span className='max-w-full min-w-0 font-mono text-xs break-all sm:wrap-break-word'>
        {props.userAgent}
      </span>
    </div>
  )
}

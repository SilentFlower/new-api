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
import type { AxiosError } from 'axios'

export function resolveApiErrorMessage(
  error: unknown,
  t: (key: string) => string
): string {
  const axiosError = error as AxiosError<{ message?: string }>
  const status = axiosError.response?.status
  if (status === 401) return t('Invalid API key')
  if (status === 403) return t('User has been banned')
  if (status === 429) return t('Too many requests. Please try again later.')
  return (
    axiosError.response?.data?.message ||
    axiosError.message ||
    t('Request failed')
  )
}

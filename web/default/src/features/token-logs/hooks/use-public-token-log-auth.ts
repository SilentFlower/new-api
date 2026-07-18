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
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { createTokenLogClient, getTokenUsage } from '../api'
import { resolveApiErrorMessage } from '../lib/errors'

export function usePublicTokenLogAuth() {
  const { t } = useTranslation()
  const [apiKey, setApiKey] = useState('')
  const [authError, setAuthError] = useState('')
  const [isAuthenticating, setIsAuthenticating] = useState(false)
  const [authenticatedKey, setAuthenticatedKey] = useState('')
  const client = useMemo(
    () => (authenticatedKey ? createTokenLogClient(authenticatedKey) : null),
    [authenticatedKey]
  )

  const handleSubmit = useCallback(async () => {
    const normalizedKey = apiKey.trim()
    if (!normalizedKey) {
      setAuthError(t('Please enter an API key'))
      return
    }
    setIsAuthenticating(true)
    setAuthError('')
    try {
      const nextClient = createTokenLogClient(normalizedKey)
      const result = await getTokenUsage(nextClient)
      if (result.code !== true) {
        setAuthError(result.message || t('Invalid API key'))
        return
      }
      setAuthenticatedKey(normalizedKey)
      setApiKey('')
    } catch (error) {
      setAuthError(resolveApiErrorMessage(error, t))
    } finally {
      setIsAuthenticating(false)
    }
  }, [apiKey, t])

  const handleSwitchKey = useCallback(() => {
    setAuthenticatedKey('')
    setApiKey('')
    setAuthError('')
  }, [])

  return {
    apiKey,
    setApiKey,
    authError,
    isAuthenticating,
    client,
    handleSubmit,
    handleSwitchKey,
  }
}

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
import { PublicLayout } from '@/components/layout'
import {
  UsageLogsProvider,
} from '@/features/usage-logs/components/usage-logs-provider'

import { AuthPanel } from './components/auth-panel'
import { TokenLogsWorkspace } from './components/token-logs-workspace'
import { usePublicTokenLogAuth } from './hooks/use-public-token-log-auth'

function PublicTokenLogsContent() {
  const auth = usePublicTokenLogAuth()

  if (!auth.client) {
    return (
      <AuthPanel
        apiKey={auth.apiKey}
        setApiKey={auth.setApiKey}
        authError={auth.authError}
        isLoading={auth.isAuthenticating}
        onSubmit={auth.handleSubmit}
      />
    )
  }

  return (
    <TokenLogsWorkspace
      client={auth.client}
      onSwitchKey={auth.handleSwitchKey}
    />
  )
}

export function PublicTokenLogs() {
  return (
    <PublicLayout showAuthButtons showThemeSwitch>
      <UsageLogsProvider>
        <PublicTokenLogsContent />
      </UsageLogsProvider>
    </PublicLayout>
  )
}

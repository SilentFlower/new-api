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
import type { Table } from '@tanstack/react-table'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Table as UiTable,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { migrateApiKeysToAccounts } from '../api'
import { ERROR_MESSAGES } from '../constants'
import type { ApiKey, ApiKeyMigrationResult } from '../types'
import { useApiKeys } from './api-keys-provider'

interface ApiKeysMigrateToAccountsDialogProps<TData> {
  open: boolean
  onOpenChange: (open: boolean) => void
  table: Table<TData>
}

export function ApiKeysMigrateToAccountsDialog<TData>({
  open,
  onOpenChange,
  table,
}: ApiKeysMigrateToAccountsDialogProps<TData>) {
  const { t } = useTranslation()
  const { triggerRefresh } = useApiKeys()
  const [isMigrating, setIsMigrating] = useState(false)
  const [results, setResults] = useState<ApiKeyMigrationResult[] | null>(null)
  const selectedApiKeys = table
    .getFilteredSelectedRowModel()
    .rows.map((row) => row.original as ApiKey)

  const handleOpenChange = (nextOpen: boolean) => {
    if (isMigrating) return
    if (!nextOpen && results) {
      table.resetRowSelection()
      triggerRefresh()
      setResults(null)
    }
    onOpenChange(nextOpen)
  }

  const handleMigrate = async () => {
    if (selectedApiKeys.length === 0) return
    setIsMigrating(true)
    try {
      const response = await migrateApiKeysToAccounts(
        selectedApiKeys.map((apiKey) => apiKey.id)
      )
      if (!response.success) {
        toast.error(response.message || t('Failed to migrate API keys'))
        return
      }
      const migrationResults = response.data?.results ?? []
      setResults(migrationResults)
      const successCount = migrationResults.filter(
        (item) => item.status === 'success'
      ).length
      toast.success(
        t('Migrated {{count}} API key(s) to standalone accounts', {
          count: successCount,
        })
      )
    } catch {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsMigrating(false)
    }
  }

  const successCount =
    results?.filter((item) => item.status === 'success').length ?? 0
  const failedCount =
    results?.filter((item) => item.status === 'failed').length ?? 0

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      title={
        results
          ? t('API key migration results')
          : t('Migrate API keys to standalone accounts?')
      }
      description={
        results
          ? t('Review the account migration result for each selected API key.')
          : t(
              'Each selected API key will be moved to a new user account. The key and group stay unchanged.'
            )
      }
      contentClassName='sm:max-w-3xl'
      contentHeight='min(52vh, 520px)'
      footerClassName='grid grid-cols-2 gap-2 sm:flex'
      footer={
        results ? (
          <Button type='button' onClick={() => handleOpenChange(false)}>
            {t('Done')}
          </Button>
        ) : (
          <>
            <Button
              type='button'
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={isMigrating}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              onClick={handleMigrate}
              disabled={isMigrating || selectedApiKeys.length === 0}
            >
              {isMigrating && <Loader2 className='animate-spin' />}
              {t('Migrate')}
            </Button>
          </>
        )
      }
    >
      {results ? (
        <div className='flex flex-col gap-3'>
          <div className='grid grid-cols-2 gap-2 sm:flex sm:flex-wrap'>
            <StatusBadge
              label={t('{{count}} succeeded', { count: successCount })}
              variant='success'
              copyable={false}
            />
            <StatusBadge
              label={t('{{count}} failed', { count: failedCount })}
              variant={failedCount > 0 ? 'danger' : 'neutral'}
              copyable={false}
            />
          </div>
          <UiTable>
            <TableHeader>
              <TableRow>
                <TableHead>{t('API Key')}</TableHead>
                <TableHead>{t('New Username')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Error')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {results.map((result) => (
                <TableRow key={result.token_id}>
                  <TableCell className='max-w-[180px] truncate font-medium'>
                    {result.token_name || `#${result.token_id}`}
                  </TableCell>
                  <TableCell className='max-w-[180px] truncate'>
                    {result.new_username || '-'}
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      label={
                        result.status === 'success' ? t('Success') : t('Failed')
                      }
                      variant={
                        result.status === 'success' ? 'success' : 'danger'
                      }
                      copyable={false}
                    />
                  </TableCell>
                  <TableCell className='max-w-[260px] whitespace-normal'>
                    {result.error || '-'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </UiTable>
        </div>
      ) : (
        <div className='flex flex-col gap-3'>
          <Alert>
            <AlertTitle>{t('Before you continue')}</AlertTitle>
            <AlertDescription>
              {t(
                'The new accounts will be created without exposing passwords in this dialog.'
              )}
            </AlertDescription>
          </Alert>
          <UiTable>
            <TableHeader>
              <TableRow>
                <TableHead>{t('API Key')}</TableHead>
                <TableHead>{t('Key')}</TableHead>
                <TableHead>{t('Group')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {selectedApiKeys.map((apiKey) => (
                <TableRow key={apiKey.id}>
                  <TableCell className='max-w-[180px] truncate font-medium'>
                    {apiKey.name}
                  </TableCell>
                  <TableCell className='max-w-[220px] truncate font-mono text-xs'>
                    {apiKey.key}
                  </TableCell>
                  <TableCell className='max-w-[140px] truncate'>
                    {apiKey.group || 'default'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </UiTable>
        </div>
      )}
    </Dialog>
  )
}

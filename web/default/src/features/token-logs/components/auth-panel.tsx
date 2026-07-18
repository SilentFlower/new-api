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
import { Key02Icon, Search01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export function AuthPanel(props: {
  apiKey: string
  setApiKey: (value: string) => void
  authError: string
  isLoading: boolean
  onSubmit: () => void
}) {
  const { t } = useTranslation()

  return (
    <Card className='mx-auto mt-16 max-w-lg rounded-lg'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <HugeiconsIcon icon={Key02Icon} strokeWidth={2} />
          {t('API Key Logs')}
        </CardTitle>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
        <div className='grid gap-2'>
          <Label htmlFor='public-log-api-key'>{t('API Key')}</Label>
          <Input
            id='public-log-api-key'
            type='password'
            autoComplete='off'
            placeholder={t('Enter API Key')}
            value={props.apiKey}
            onChange={(event) => props.setApiKey(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') props.onSubmit()
            }}
          />
        </div>
        {props.authError && (
          <Alert variant='destructive'>
            <AlertTitle>{t('Authentication failed')}</AlertTitle>
            <AlertDescription>{props.authError}</AlertDescription>
          </Alert>
        )}
        <Button
          type='button'
          className='w-full'
          onClick={props.onSubmit}
          disabled={props.isLoading}
        >
          {props.isLoading ? (
            <LoadingState inline size='sm' message={t('Verifying...')} />
          ) : (
            <>
              <HugeiconsIcon
                icon={Search01Icon}
                data-icon='inline-start'
                strokeWidth={2}
              />
              {t('View Logs')}
            </>
          )}
        </Button>
      </CardContent>
    </Card>
  )
}

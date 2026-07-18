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
import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function StatBadgeCard(props: {
  label: string
  value: string
  description?: string
  accent: string
}) {
  return (
    <Card size='sm' className='rounded-lg'>
      <CardContent className='flex items-center gap-3'>
        <span className={cn('h-9 w-1 rounded-full', props.accent)} />
        <div className='min-w-0'>
          <p className='text-muted-foreground text-xs'>{props.label}</p>
          <p className='text-foreground truncate font-mono text-lg font-semibold tabular-nums'>
            {props.value}
          </p>
          {props.description && (
            <p className='text-muted-foreground/70 truncate text-xs'>
              {props.description}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

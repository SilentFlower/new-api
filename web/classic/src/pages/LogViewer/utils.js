/*
Copyright (C) 2025 QuantumNous

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

import {
  getTodayStartTimestamp,
} from '../../helpers';

export function getGlobalTokenLogTimeRange(timePreset, customDateRange) {
  const now = Math.floor(Date.now() / 1000);
  if (
    timePreset === 'custom' &&
    customDateRange &&
    customDateRange.length === 2
  ) {
    return {
      startTs: Math.floor(new Date(customDateRange[0]).getTime() / 1000),
      endTs: Math.floor(new Date(customDateRange[1]).getTime() / 1000),
    };
  }
  const presetMap = {
    today: getTodayStartTimestamp(),
    '7d': now - 7 * 86400,
    '30d': now - 30 * 86400,
  };
  return {
    startTs: Math.floor(presetMap[timePreset] || presetMap.today),
    endTs: now,
  };
}

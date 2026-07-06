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

const DASHBOARD_TOKEN_VALUE_SEPARATOR = '\0';

export const normalizeSelectValues = (value) => {
  if (Array.isArray(value)) {
    return value;
  }
  if (value === undefined || value === null || value === '') {
    return [];
  }
  return [value];
};

export const buildDashboardTokenOptionValue = (
  tokenName,
  username = '',
  group = '',
) =>
  [tokenName || '', username || '', group || ''].join(
    DASHBOARD_TOKEN_VALUE_SEPARATOR,
  );

export const extractDashboardTokenName = (value) =>
  String(value || '')
    .split(DASHBOARD_TOKEN_VALUE_SEPARATOR)[0]
    .trim();

export const getDashboardTokenOptionGroup = (option) => {
  if (option?.group !== undefined && option?.group !== null) {
    return String(option.group);
  }
  return (
    String(option?.value || '').split(DASHBOARD_TOKEN_VALUE_SEPARATOR)[2] || ''
  );
};

export const filterDashboardTokenOptionsByGroups = (options, groups) => {
  const selectedGroups = normalizeSelectValues(groups);
  if (selectedGroups.length === 0) {
    return options;
  }
  const groupSet = new Set(selectedGroups.map(String));
  return options.filter((option) =>
    groupSet.has(getDashboardTokenOptionGroup(option)),
  );
};

export const filterDashboardTokenValuesByGroups = (values, groups, options) => {
  const selectedValues = normalizeSelectValues(values);
  const filteredOptions = filterDashboardTokenOptionsByGroups(options, groups);
  if (normalizeSelectValues(groups).length === 0) {
    return selectedValues;
  }
  const allowedValues = new Set(filteredOptions.map((option) => option.value));
  return selectedValues.filter((value) => allowedValues.has(value));
};

export const appendDashboardFilterParams = (params, name, values, mapper) => {
  const seen = new Set();
  normalizeSelectValues(values).forEach((value) => {
    const normalized = String(mapper ? mapper(value) : value).trim();
    if (!normalized || seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    params.append(name, normalized);
  });
};

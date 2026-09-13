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
import assert from 'node:assert/strict'

import {
  channelPolicyErrorDetail,
  matchChannelPolicyErrorRows,
} from '../channel-policy-error-rows'

const testModule = 'bun:test'
const { test } = (await import(testModule)) as {
  test: typeof import('node:test').test
}

const rows = [
  { id: 'b1', name: '池子每日' },
  { id: 'b2', name: '假期个人每日' },
  { id: 'b3', name: '假期个人' },
]

test('「原因：行名」格式按整段精确匹配，不把行名互为前缀的其他行一起标出', () => {
  assert.deepEqual(
    matchChannelPolicyErrorRows(
      '渠道周期策略参数无效: 预算引用的时段不存在：假期个人每日',
      rows
    ),
    ['b2']
  )
})

test('重复预算的「A / B」格式同时标出两行', () => {
  assert.deepEqual(
    matchChannelPolicyErrorRows(
      '渠道周期策略参数无效: 同一范围、周期与模型的预算重复：池子每日 / 假期个人',
      rows
    ),
    ['b1', 'b3']
  )
})

test('「行名 的超限动作无效：原因」格式取前面的行名', () => {
  assert.deepEqual(
    matchChannelPolicyErrorRows(
      '渠道周期策略参数无效: 池子每日 的超限动作无效：降级目标渠道及模型无效',
      rows
    ),
    ['b1']
  )
})

test('没有点名任何行或草稿行没有名称时不标出行；整段不匹配时退回子串匹配', () => {
  assert.deepEqual(
    matchChannelPolicyErrorRows('渠道周期策略参数无效: 策略版本无效', rows),
    []
  )
  assert.deepEqual(
    matchChannelPolicyErrorRows(
      '渠道周期策略参数无效: 预算引用的时段不存在：X',
      [{ id: 'b9', name: '  ' }]
    ),
    []
  )
  assert.deepEqual(
    matchChannelPolicyErrorRows('预算「池子每日」额度无效', rows),
    ['b1']
  )
})

test('去掉哨兵前缀只留具体原因，没有前缀时原样返回', () => {
  assert.equal(
    channelPolicyErrorDetail(
      '渠道周期策略参数无效: 预算引用的时段不存在：假期个人每日'
    ),
    '预算引用的时段不存在：假期个人每日'
  )
  assert.equal(channelPolicyErrorDetail('策略已变更'), '策略已变更')
})

import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { TFunction } from 'i18next'

import { getUserFormSchema } from '../user-form'

const t = ((key: string) => key) as TFunction

describe('用户表单名称长度', () => {
  test('64 个中文码点可以提交，65 个会拒绝', () => {
    const schema = getUserFormSchema(t)
    assert.equal(schema.safeParse({ username: '中'.repeat(64) }).success, true)
    assert.equal(schema.safeParse({ username: '中'.repeat(65) }).success, false)
  })

  test('显示名按码点计数而非 UTF-16 单元数', () => {
    const schema = getUserFormSchema(t)
    assert.equal(schema.safeParse({ username: '工作流', display_name: '😀'.repeat(64) }).success, true)
    assert.equal(schema.safeParse({ username: '工作流', display_name: '😀'.repeat(65) }).success, false)
  })
})

import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { TFunction } from 'i18next'

import { API_KEY_FORM_DEFAULT_VALUES, getApiKeyFormSchema } from '../api-key-form'

const t = ((key: string) => key) as TFunction

describe('令牌名称长度', () => {
  test('64 个补充平面码点可提交，65 个会拒绝', () => {
    const schema = getApiKeyFormSchema(t)
    assert.equal(schema.safeParse({ ...API_KEY_FORM_DEFAULT_VALUES, name: '😀'.repeat(64) }).success, true)
    assert.equal(schema.safeParse({ ...API_KEY_FORM_DEFAULT_VALUES, name: '😀'.repeat(65) }).success, false)
  })
})

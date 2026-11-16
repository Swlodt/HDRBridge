import assert from 'node:assert/strict'
import test from 'node:test'
import { changePhotos, parseUploadResult } from './api.ts'

test('请求超限时仍保留已经入库的图片', () => {
  const result = parseUploadResult(413, JSON.stringify({ images: [{ id: 'accepted' }], error: '请求超限' }))
  assert.equal(result.images?.[0].id, 'accepted')
  assert.equal(result.error, '请求超限')
})

test('部分成功响应保留逐文件错误', () => {
  const result = parseUploadResult(201, JSON.stringify({ images: [{ id: 'accepted' }], errors: [{ name: 'bad.jxr', error: '文件损坏', status: 400 }] }))
  assert.equal(result.images?.length, 1)
  assert.equal(result.errors?.[0].error, '文件损坏')
})

test('不明确的上传结果提示先确认相册, 不自动重传', () => {
  for (const response of ['<html>Bad gateway</html>', '{}', 'null']) {
    assert.match(parseUploadResult(200, response).error ?? '', /确认/)
  }
  assert.equal(parseUploadResult(503, '{}').error, '上传失败 (503)')
})

test('批量移动继续处理后续图片并保留可重试的失败项', async t => {
  const photos = ['first', 'failed', 'last'].map(id => ({ id, original_name: `${id}.jxr` }))
  const fetchMock = t.mock.method(globalThis, 'fetch', async (path: string, init: RequestInit) => {
    assert.equal(init.method, 'PATCH')
    assert.deepEqual(JSON.parse(String(init.body)), { album_id: 'target' })
    if (path.endsWith('/failed')) return Response.json({ error: '服务器内部错误' }, { status: 500 })
    return Response.json({ album_id: 'target' })
  })
  const progress: number[] = []
  const result = await changePhotos(photos, { kind: 'move', albumID: 'target' }, count => progress.push(count))
  assert.deepEqual(fetchMock.mock.calls.map(call => call.arguments[0]), photos.map(photo => `/api/images/${photo.id}`))
  assert.deepEqual(result.completed, ['first', 'last'])
  assert.deepEqual(result.failed, [{ photo: photos[1], message: '服务器内部错误' }])
  assert.deepEqual(progress, [1, 2, 3])
})

test('批量删除重试时将已不存在的图片视为完成', async t => {
  t.mock.method(globalThis, 'fetch', async (_: string, init: RequestInit) => {
    assert.equal(init.method, 'DELETE')
    return Response.json({ error: '资源不存在' }, { status: 404 })
  })
  const result = await changePhotos([{ id: 'gone', original_name: 'gone.jxr' }], { kind: 'delete' }, () => {})
  assert.deepEqual(result, { completed: ['gone'], failed: [] })
})

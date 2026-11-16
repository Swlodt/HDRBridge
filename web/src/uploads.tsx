import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { errorMessage, libraryImagesKey, uploadFile } from './api'
import type { Album } from './api'
import { Icon, Spinner } from './ui'

interface UploadItem {
  id: number
  name: string
  album: string
  status: 'waiting' | 'uploading' | 'done' | 'error'
  progress: number
  error?: string
}

export function useUploads() {
  const client = useQueryClient()
  const [items, setItems] = useState<UploadItem[]>([])
  const [expanded, setExpanded] = useState(true)
  const queue = useRef<{ id: number; file: File; albumID: string }[]>([])
  const running = useRef(false)
  const sequence = useRef(0)
  const controller = useRef<AbortController | null>(null)
  const mounted = useRef(true)
  const active = items.some(item => item.status === 'waiting' || item.status === 'uploading')

  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false; queue.current = []; controller.current?.abort() }
  }, [])
  useEffect(() => {
    if (!active) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [active])

  const update = (id: number, patch: Partial<UploadItem>) => {
    if (mounted.current) setItems(previous => previous.map(item => item.id === id ? { ...item, ...patch } : item))
  }

  async function drain() {
    if (running.current) return
    running.current = true
    while (mounted.current && queue.current.length) {
      const next = queue.current.shift()!
      controller.current = new AbortController()
      update(next.id, { status: 'uploading' })
      try {
        const result = await uploadFile(next.albumID, next.file, value => update(next.id, { progress: value }), controller.current.signal)
        const photo = result.images?.[0]
        const issue = result.error || result.errors?.map(error => error.error).join('；')
        update(next.id, { status: photo ? 'done' : 'error', progress: 100, error: issue })
      } catch (error) {
        update(next.id, { status: 'error', error: errorMessage(error) })
      }
      // 上传所属相册在选择文件时固定, 切换页面不会改变目标或中断队列
      void client.invalidateQueries({ queryKey: ['images', next.albumID] })
      void client.invalidateQueries({ queryKey: libraryImagesKey })
    }
    controller.current = null
    running.current = false
  }

  function add(files: File[], album: Album) {
    const additions: UploadItem[] = files.map(file => {
      const id = ++sequence.current
      const error = file.size === 0 ? '文件为空' : undefined
      if (!error) queue.current.push({ id, file, albumID: album.id })
      return { id, name: file.name, album: album.name, status: error ? 'error' : 'waiting', progress: 0, error }
    })
    setItems(previous => [...previous, ...additions])
    setExpanded(true)
    void drain()
  }

  return {
    add, active,
    panel: items.length > 0 && <section className="upload-panel" aria-label="上传队列">
      <div className="upload-heading">
        <button className="upload-toggle" onClick={() => setExpanded(!expanded)} aria-expanded={expanded}>
          {active ? <Spinner /> : <Icon name={items.some(item => item.status === 'error' || item.error) ? 'warning' : 'check'} />}
          <span>{active ? '正在上传' : '上传结果'}<small>{items.filter(item => item.status === 'done').length} / {items.length}</small></span>
          <Icon name="chevron" className={expanded ? 'rotate-up' : 'rotate-down'} />
        </button>
        {!active && <button className="icon-button" aria-label="关闭上传队列" onClick={() => setItems([])}><Icon name="close" /></button>}
      </div>
      {expanded && <><div className="upload-list">{items.map(item => <div className="upload-item" key={item.id}>
        <Icon name={item.status === 'error' ? 'warning' : item.status === 'done' ? 'check' : 'image'} />
        <div className="upload-item-copy"><strong title={item.name}>{item.name}</strong><small>{item.album} · {uploadStatus(item)}</small>
          {item.error && <p className="upload-error">{item.error}</p>}
          {item.status === 'uploading' && <progress value={item.progress} max="100" aria-label={`${item.name} 上传进度`} />}
        </div>
      </div>)}</div><p className="upload-footnote" role="status">{active ? '请保持页面打开，直到图片保存完成。' : items.some(item => item.status === 'done') ? '可以在相册中查看已上传的图片。' : '没有图片上传成功，请查看错误说明后重新选择文件。'}</p></>}
    </section>,
  }
}

function uploadStatus(item: UploadItem) {
  switch (item.status) {
    case 'done': return '上传完成'
    case 'error': return '上传失败'
    case 'waiting': return '等待上传'
    case 'uploading':
      if (item.progress < 100) return `${item.progress}%`
      return /\.jxr$/i.test(item.name) ? '正在处理 JXR 转换' : '正在保存图片'
  }
}

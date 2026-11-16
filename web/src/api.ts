export interface Album {
  id: string
  name: string
  description: string
  created_at: string
  updated_at: string
}

export interface Photo {
  id: string
  album_id: string
  original_name: string
  source_media_type: string
  heic_has_gain_map?: boolean
  created_at: string
  updated_at: string
}

export interface ImagePage {
  images: Photo[]
  next_cursor: string | null
  matched: number
  total: number
}

export class APIError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'APIError'
    this.status = status
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api${path}`, {
    ...init,
    headers: { ...(init?.body ? { 'Content-Type': 'application/json' } : {}), ...init?.headers },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => null)
    throw new APIError(body?.error || `请求失败 (${response.status})`, response.status)
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const albumPath = (id: string) => `/albums/${encodeURIComponent(id)}`
export const imagePath = (id: string) => `/images/${encodeURIComponent(id)}`
export const libraryPath = '/images'
export const libraryImagesKey = ['images', null] as const
export const acceptedImageFiles = '.jxr,.jpg,.jpeg,.jpe,.jfif,.png,.apng,.gif,.webp,.avif,.bmp,.ico,.svg,.tif,.tiff,.heic,.heif,.heics,.jxl'
export const previewURL = (photo: Photo, fullSize: boolean) => `/api${imagePath(photo.id)}/${fullSize ? 'preview' : 'thumbnail'}?v=${encodeURIComponent(photo.updated_at)}`
export const downloadURL = (id: string) => `/api${imagePath(id)}/download`
export const isJXR = (photo: Photo) => photo.source_media_type === 'image/jxr'
export const convertedHEICLabel = (photo: Photo) => {
  if (photo.heic_has_gain_map === true) return 'HDR HEIC'
  if (photo.heic_has_gain_map === false) return 'SDR HEIC'
  return 'HEIC'
}
const formatNames: Record<string, string> = {
  'image/jxr': 'JXR', 'image/jpeg': 'JPEG', 'image/png': 'PNG', 'image/gif': 'GIF',
  'image/webp': 'WebP', 'image/avif': 'AVIF', 'image/bmp': 'BMP', 'image/x-icon': 'ICO',
  'image/svg+xml': 'SVG', 'image/tiff': 'TIFF', 'image/heic': 'HEIC', 'image/heif': 'HEIF',
  'image/heic-sequence': 'HEIC', 'image/heif-sequence': 'HEIF', 'image/jxl': 'JPEG XL',
}
export const photoFormat = (photo: Photo) => formatNames[photo.source_media_type] ?? '图片'
export const errorMessage = (error: unknown) => error instanceof Error ? error.message : '操作失败，请稍后重试'

export type PhotoAction = { kind: 'move'; albumID: string } | { kind: 'delete' }
export type BatchPhoto = Pick<Photo, 'id' | 'original_name'>

export function changePhoto(id: string, action: PhotoAction): Promise<Photo | void> {
  if (action.kind === 'delete') return request<void>(imagePath(id), { method: 'DELETE' })
  return request<Photo>(imagePath(id), { method: 'PATCH', body: JSON.stringify({ album_id: action.albumID }) })
}

// changePhotos 复用单图接口并逐张执行, 让失败项可以独立重试
export async function changePhotos(photos: BatchPhoto[], action: PhotoAction, onProgress: (processed: number) => void) {
  const completed: string[] = []
  const failed: { photo: BatchPhoto; message: string }[] = []
  for (const photo of photos) {
    try {
      await changePhoto(photo.id, action)
      completed.push(photo.id)
    } catch (error) {
      if (error instanceof APIError && error.status === 404) {
        // 删除响应丢失后可能重试, 图片已不存在也视为完成
        if (action.kind === 'delete') completed.push(photo.id)
        else failed.push({ photo, message: '图片或目标相册已不存在' })
      } else {
        failed.push({ photo, message: error instanceof APIError ? error.message : '无法确认操作结果，请重试' })
      }
    }
    onProgress(completed.length + failed.length)
  }
  return { completed, failed }
}

export interface UploadResponse {
  images?: Photo[]
  errors?: { name: string; error: string; status: number }[]
  error?: string
}

// parseUploadResult 保留非 2xx 响应中已入库的图片, 避免客户端重复上传
export function parseUploadResult(status: number, text: string): UploadResponse {
  try {
    const body: UploadResponse = JSON.parse(text)
    if (!body || typeof body !== 'object') throw new Error('invalid response')
    if (status >= 400 && !body.error && !body.errors?.length) {
      body.error = `上传失败 (${status})`
    }
    if (!body.images?.length && !body.errors?.length && !body.error) {
      body.error = '服务未返回上传结果，请刷新相册确认'
    }
    return body
  } catch {
    return { error: `无法读取上传结果 (${status})，请刷新相册确认后再重试` }
  }
}

export function uploadFile(albumID: string, file: File, progress: (value: number) => void, signal: AbortSignal): Promise<UploadResponse> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const abort = () => xhr.abort()
    if (signal.aborted) { reject(new Error('上传已取消')); return }
    signal.addEventListener('abort', abort, { once: true })
    xhr.open('POST', `/api${albumPath(albumID)}/images`)
    xhr.timeout = 30 * 60 * 1000
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) progress(Math.min(99, Math.round(event.loaded / event.total * 100)))
    }
    xhr.upload.onload = () => progress(100)
    xhr.onload = () => resolve(parseUploadResult(xhr.status, xhr.responseText))
    xhr.onerror = () => reject(new Error('连接中断，请刷新相册确认上传结果'))
    xhr.ontimeout = () => reject(new Error('上传超时，请刷新相册确认上传结果'))
    xhr.onabort = () => reject(new Error('上传已取消，请刷新相册确认上传结果'))
    xhr.onloadend = () => signal.removeEventListener('abort', abort)
    const form = new FormData()
    form.append('files', file)
    xhr.send(form)
  })
}

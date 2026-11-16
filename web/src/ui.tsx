import { useEffect, useId, useRef, useState } from 'react'
import type { ReactNode, SVGProps } from 'react'
import type { Photo } from './api'
import { convertedHEICLabel, isJXR, previewURL } from './api'

const paths = {
  plus: <path d="M12 5v14M5 12h14" />,
  close: <path d="m6 6 12 12M18 6 6 18" />,
  album: <><rect x="4" y="4" width="16" height="16" rx="2" /><path d="M8 4v16M12 9h4M12 13h4" /></>,
  inbox: <><path d="m5 5-3 9v5h20v-5l-3-9H5Z" /><path d="M2 14h6l2 3h4l2-3h6" /></>,
  upload: <><path d="M12 16V3m-5 5 5-5 5 5M4 15v5h16v-5" /></>,
  download: <><path d="M12 3v13m-5-5 5 5 5-5M4 16v5h16v-5" /></>,
  image: <><rect x="3" y="3" width="18" height="18" rx="2" /><circle cx="8" cy="8" r="1.5" /><path d="m3 17 5-5 4 4 4-6 5 7" /></>,
  grid: <><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></>,
  search: <><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 5 5" /></>,
  more: <><circle cx="5" cy="12" r="1" /><circle cx="12" cy="12" r="1" /><circle cx="19" cy="12" r="1" /></>,
  chevron: <path d="m9 5 7 7-7 7" />,
  check: <path d="m5 12 4 4L19 6" />,
  warning: <><circle cx="12" cy="12" r="9" /><path d="M12 7v6m0 3h.01" /></>,
  retry: <><path d="M20 7v5h-5M4 17v-5h5" /><path d="M6.1 6.1A8 8 0 0 1 20 12M4 12a8 8 0 0 0 13.9 5.9" /></>,
  trash: <><path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7" /></>,
  move: <><path d="M10 5H3v14h7m2-7h9m-4-4 4 4-4 4" /></>,
  edit: <><path d="m16 3 5 5L8 21H3v-5L16 3ZM13 6l5 5" /></>,
  menu: <path d="M4 6h16M4 12h16M4 18h16" />,
  bridge: <><path d="M4 4v16M20 4v16M12 4v16M4 12h16" /></>,
} satisfies Record<string, ReactNode>

export function Icon({ name, ...props }: SVGProps<SVGSVGElement> & { name: keyof typeof paths }) {
  return <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>{paths[name]}</svg>
}

export function Spinner() { return <span className="spinner" aria-hidden="true" /> }

export function PhotoPreview({ photo, large = false }: { photo: Photo; large?: boolean }) {
  const [failure, setFailure] = useState({ version: '', full: false, thumbnail: false })
  const failed = failure.version === photo.updated_at ? failure : { version: photo.updated_at, full: false, thumbnail: false }
  const full = large && !failed.full
  if (full || !failed.thumbnail) {
    return <img src={previewURL(photo, full)} alt={photo.original_name} loading={large ? 'eager' : 'lazy'} decoding="async" onError={() => setFailure({ ...failed, [full ? 'full' : 'thumbnail']: true })} />
  }
  return <div className="thumbnail-placeholder">
    <Icon name="image" />
    <span>暂无预览</span>
    {large && <small>{isJXR(photo) ? `仍可下载 ${convertedHEICLabel(photo)} 查看` : '仍可下载原图查看'}</small>}
  </div>
}

export function Modal({ title, children, onClose, busy = false, className = '' }: {
  title: string; children: ReactNode; onClose: () => void; busy?: boolean; className?: string
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleID = useId()
  useEffect(() => {
    const dialog = ref.current!
    const previous = document.activeElement as HTMLElement | null
    dialog.showModal()
    dialog.querySelector<HTMLElement>('input:not([type=radio]), textarea, select')?.focus()
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      dialog.close()
      document.body.style.overflow = overflow
      if (previous?.isConnected && previous.getClientRects().length) previous.focus()
      else document.querySelector<HTMLElement>('main h1')?.focus()
    }
  }, [])
  return <dialog ref={ref} className={`modal ${className}`} aria-labelledby={titleID}
    onCancel={event => { event.preventDefault(); if (!busy) onClose() }}>
    <div className="modal-heading"><h2 id={titleID}>{title}</h2><button className="icon-button" aria-label="关闭对话框" onClick={onClose} disabled={busy}><Icon name="close" /></button></div>
    {children}
  </dialog>
}

export function ErrorNotice({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return <div className="error-notice" role="alert"><Icon name="warning" /><span>{message}</span>{onRetry && <button className="text-button" onClick={onRetry}>重试</button>}</div>
}

export function formatDate(value: string, detailed = false) {
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: 'long', day: 'numeric', ...(detailed ? { hour: '2-digit', minute: '2-digit' } as const : {}),
  }).format(new Date(value))
}

import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { albumPath, APIError, changePhoto, changePhotos, convertedHEICLabel, downloadURL, errorMessage, imagePath, isJXR, photoFormat, request } from './api'
import type { Album, BatchPhoto, Photo, PhotoAction } from './api'
import { ErrorNotice, formatDate, Icon, Modal, PhotoPreview, Spinner } from './ui'

type DialogProps = { onClose: () => void; notify: (message: string) => void }

export function AlbumEditor({ album, onClose, notify }: DialogProps & { album?: Album }) {
  const [name, setName] = useState(album?.name ?? '')
  const [description, setDescription] = useState(album?.description ?? '')
  const client = useQueryClient()
  const navigate = useNavigate()
  const mutation = useMutation({
    mutationFn: () => request<Album>(album ? albumPath(album.id) : '/albums', {
      method: album ? 'PATCH' : 'POST', body: JSON.stringify({ name: name.trim(), description: description.trim() }),
    }),
    onSuccess: async result => {
      await client.invalidateQueries({ queryKey: ['albums'] })
      navigate(albumPath(result.id))
      notify(album ? '相册已更新' : '相册已创建')
      onClose()
    },
  })
  function submit(event: FormEvent) { event.preventDefault(); mutation.mutate() }
  return <Modal title={album ? '编辑相册' : '新建相册'} onClose={onClose} busy={mutation.isPending}>
    <form onSubmit={submit} className="dialog-form">
      <p className="dialog-intro">{album ? '为这段光影，留下一个名字。' : '给你的图片一个新的归处。'}</p>
      <label>相册名称<input required maxLength={120} value={name} onChange={event => setName(event.target.value)} placeholder="例如：山间的光" disabled={mutation.isPending} /></label>
      <label>描述 <span className="optional">可选</span><textarea maxLength={2000} rows={3} value={description} onChange={event => setDescription(event.target.value)} placeholder="记录地点、时间，或这一刻的感受" disabled={mutation.isPending} /></label>
      {mutation.isError && <ErrorNotice message={errorMessage(mutation.error)} />}
      <div className="dialog-actions"><button type="button" className="button secondary" onClick={onClose} disabled={mutation.isPending}>取消</button><button className="button primary" disabled={!name.trim() || mutation.isPending}>{mutation.isPending && <Spinner />}{album ? '保存更改' : '创建相册'}</button></div>
    </form>
  </Modal>
}

export function DeleteAlbum({ album, onClose, notify }: DialogProps & { album: Album }) {
  const [deleteImages, setDeleteImages] = useState(false)
  const client = useQueryClient()
  const navigate = useNavigate()
  const mutation = useMutation({
    mutationFn: () => request<void>(`${albumPath(album.id)}?delete_images=${deleteImages}`, { method: 'DELETE' }),
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['albums'] }),
        client.invalidateQueries({ queryKey: ['images'] }),
        client.invalidateQueries({ queryKey: ['image'] }),
      ])
      navigate(albumPath('uncategorized'), { replace: true })
      notify(deleteImages ? '相册和图片已删除' : '相册已删除，图片已移至未分类')
      onClose()
    },
  })
  return <Modal title="删除相册" onClose={onClose} busy={mutation.isPending}>
    <div className="dialog-form">
      <p className="dialog-intro">删除「{album.name}」后，如何处理其中的图片？</p>
      <label className={`choice ${!deleteImages ? 'chosen' : ''}`}><input type="radio" name="delete-album-mode" checked={!deleteImages} onChange={() => setDeleteImages(false)} disabled={mutation.isPending} /><span><strong>保留图片</strong><small>图片移至「未分类」。</small></span></label>
      <label className={`choice ${deleteImages ? 'chosen' : ''}`}><input type="radio" name="delete-album-mode" checked={deleteImages} onChange={() => setDeleteImages(true)} disabled={mutation.isPending} /><span><strong>同时删除所有图片</strong><small>原始文件和转换结果会永久删除，无法恢复。</small></span></label>
      {mutation.isError && <ErrorNotice message={errorMessage(mutation.error)} />}
      <div className="dialog-actions"><button className="button secondary" onClick={onClose} disabled={mutation.isPending}>取消</button><button className="button danger" onClick={() => mutation.mutate()} disabled={mutation.isPending}>{mutation.isPending && <Spinner />}{deleteImages ? '删除相册及图片' : '删除相册'}</button></div>
    </div>
  </Modal>
}

export function BatchPhotos({ kind, photos, destinations, onCompleted, onClose, notify }: DialogProps & {
  kind: PhotoAction['kind']; photos: BatchPhoto[]; destinations: Album[]; onCompleted: (ids: string[]) => void
}) {
  const client = useQueryClient()
  const [target, setTarget] = useState('')
  const [processed, setProcessed] = useState(0)
  const moving = kind === 'move'
  const title = moving ? '批量移动' : '批量删除'
  const mutation = useMutation({
    mutationFn: ({ items, action }: { items: BatchPhoto[]; action: PhotoAction }) => changePhotos(items, action, setProcessed),
    onSuccess: async ({ completed, failed }) => {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['images'] }),
        client.invalidateQueries({ queryKey: ['image'] }),
      ])
      onCompleted(completed)
      if (failed.length) return
      notify(`已${moving ? '移动' : '删除'} ${completed.length} 张图片`)
      onClose()
    },
  })
  const failed = mutation.data?.failed
  const pendingPhotos = failed ? failed.map(item => item.photo) : photos
  const busy = mutation.isPending
  const validTarget = destinations.some(album => album.id === target)

  function submit(event: FormEvent) {
    event.preventDefault()
    if (busy || !pendingPhotos.length || (moving && !validTarget)) return
    setProcessed(0)
    mutation.mutate({ items: pendingPhotos, action: moving ? { kind, albumID: target } : { kind } })
  }

  return <Modal title={title} onClose={onClose} busy={busy}>
    <form onSubmit={submit} className="dialog-form">
      {moving ? <>
        <p className="dialog-intro">将 {pendingPhotos.length} 张图片移动到同一个相册。</p>
        <label>目标相册<select value={target} onChange={event => setTarget(event.target.value)} disabled={busy} required>
          <option value="" disabled>选择相册</option>
          {destinations.map(album => <option key={album.id} value={album.id}>{album.name}</option>)}
        </select></label>
      </> : <p className="dialog-intro">永久删除 {pendingPhotos.length} 张图片的原始文件及转换结果。此操作无法恢复。</p>}
      {busy && <p className="batch-progress" role="status"><Spinner />已处理 {processed} / {mutation.variables?.items.length} 张图片</p>}
      {failed && failed.length > 0 && <>
        <ErrorNotice message={`本次成功 ${mutation.data?.completed.length} 张，失败 ${failed.length} 张。`} />
        <ul className="batch-errors">{failed.map(({ photo, message }) => <li key={photo.id}>{photo.original_name} · {message}</li>)}</ul>
      </>}
      <div className="dialog-actions">
        <button type="button" className="button secondary" onClick={onClose} disabled={busy}>{failed ? '关闭' : '取消'}</button>
        <button className={`button ${moving ? 'primary' : 'danger'}`} disabled={busy || (moving && !validTarget)}>{failed ? '重试失败项' : moving ? '确认移动' : '确认删除'}</button>
      </div>
    </form>
  </Modal>
}

export function PhotoDetail({ id, albums, onClose, notify, onPrevious, onNext }: DialogProps & {
  id: string; albums: Album[]; onPrevious?: () => void; onNext?: () => void
}) {
  const client = useQueryClient()
  const [target, setTarget] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const query = useQuery({
    queryKey: ['image', id],
    queryFn: ({ signal }) => request<Photo>(imagePath(id), { signal }),
  })
  const photo = query.data
  const mutation = useMutation({
    mutationFn: (action: 'move' | 'delete') => changePhoto(id, action === 'move' ? { kind: action, albumID: target } : { kind: action }),
    onSuccess: async (result, action) => {
      if (result) client.setQueryData(['image', id], result)
      await client.invalidateQueries({ queryKey: ['images'] })
      notify(action === 'move' ? '图片已移至目标相册' : '图片已删除')
      onClose()
    },
  })
  const busy = mutation.isPending
  const jxr = photo !== undefined && isJXR(photo)
  const heicLabel = jxr ? convertedHEICLabel(photo) : null
  const previewLabel = photo?.heic_has_gain_map === true ? 'SDR 预览 · 下载查看 HDR' : jxr ? 'SDR 预览' : '图片预览'
  return <Modal title="图片详情" onClose={onClose} busy={busy} className="photo-modal">
    {!photo ? <div className="detail-loading">{query.isError ? <ErrorNotice message={query.error instanceof APIError && query.error.status === 404 ? '图片已删除或不存在' : errorMessage(query.error)} onRetry={() => void query.refetch()} /> : <><Spinner /><span>正在载入图片…</span></>}</div> : <div className="photo-detail">
      <div className="preview-stage">
        <PhotoPreview photo={photo} large />
        <div className="preview-navigation"><button className="icon-button" aria-label="上一张图片" onClick={onPrevious} disabled={!onPrevious || busy || confirmDelete}><Icon name="chevron" className="rotate-back" /></button><span>{previewLabel}</span><button className="icon-button" aria-label="下一张图片" onClick={onNext} disabled={!onNext || busy || confirmDelete}><Icon name="chevron" /></button></div>
      </div>
      <div className="detail-info">
        <span className="eyebrow">{photoFormat(photo)}</span><h3>{photo.original_name}</h3>
        <dl><div><dt>所属相册</dt><dd>{albums.find(album => album.id === photo.album_id)?.name ?? '相册已变更'}</dd></div><div><dt>添加时间</dt><dd>{formatDate(photo.created_at, true)}</dd></div>{heicLabel && <div><dt>转换格式</dt><dd>{heicLabel}</dd></div>}</dl>
        {query.isError && <ErrorNotice message="图片信息刷新失败，请检查连接" onRetry={() => void query.refetch()} />}
        <a className="button primary download-button" href={downloadURL(id)} download><Icon name="download" />{heicLabel ? `下载 ${heicLabel}` : '下载原图'}</a>
        <div className="move-control"><label htmlFor="target-album">移动到相册</label><div><select id="target-album" value={target} onChange={event => setTarget(event.target.value)} disabled={busy}><option value="">选择相册</option>{albums.filter(album => album.id !== photo.album_id).map(album => <option value={album.id} key={album.id}>{album.name}</option>)}</select><button className="icon-button outlined" aria-label="移动图片" disabled={!target || busy} onClick={() => mutation.mutate('move')}><Icon name="move" /></button></div></div>
        {mutation.isError && <ErrorNotice message={errorMessage(mutation.error)} />}
        {confirmDelete ? <div className="delete-confirm"><p>{jxr ? '永久删除这张图片及其转换结果？' : '永久删除这张图片？'}此操作无法恢复。</p><div><button className="button secondary" disabled={busy} onClick={() => setConfirmDelete(false)}>取消</button><button className="button danger" disabled={busy} onClick={() => mutation.mutate('delete')}>{busy && <Spinner />}确认删除</button></div></div> : <button className="text-button delete-photo" disabled={busy} onClick={() => setConfirmDelete(true)}><Icon name="trash" />删除图片</button>}
      </div>
    </div>}
  </Modal>
}

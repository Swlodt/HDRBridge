import { useEffect, useRef, useState } from 'react'
import type { DragEvent } from 'react'
import { NavLink, Navigate, Route, Routes, useMatch, useNavigate, useParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { acceptedImageFiles, albumPath, APIError, convertedHEICLabel, errorMessage, isJXR, libraryImagesKey, libraryPath, photoFormat, request } from './api'
import type { Album, BatchPhoto, ImagePage, PhotoAction } from './api'
import { AlbumEditor, BatchPhotos, DeleteAlbum, PhotoDetail } from './dialogs'
import { ErrorNotice, Icon, PhotoPreview, Spinner } from './ui'
import { useUploads } from './uploads'

type Overlay = { kind: 'create' } | { kind: 'edit' | 'delete'; album: Album }

export function App() {
  const inLibrary = useMatch(libraryPath) !== null
  const albums = useQuery({ queryKey: ['albums'], queryFn: ({ signal }) => request<Album[]>('/albums', { signal }) })
  const [overlay, setOverlay] = useState<Overlay | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [toast, setToast] = useState('')
  const uploads = useUploads()
  const sidebarButton = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!toast) return
    const timer = window.setTimeout(() => setToast(''), 4000)
    return () => window.clearTimeout(timer)
  }, [toast])
  useEffect(() => {
    if (!sidebarOpen) return
    const close = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { setSidebarOpen(false); sidebarButton.current?.focus() }
    }
    window.addEventListener('keydown', close)
    return () => window.removeEventListener('keydown', close)
  }, [sidebarOpen])
  function closeSidebar() {
    setSidebarOpen(false)
    if (sidebarOpen && window.matchMedia('(max-width: 820px)').matches) sidebarButton.current?.focus()
  }
  const workspace = <GalleryRoute albums={albums.data ?? []} albumsLoading={albums.isPending} albumsError={albums.error} refreshAlbums={() => void albums.refetch()} onOverlay={setOverlay} onUpload={uploads.add} notify={setToast} />
  return <div className="app-shell">
    <a href="#main" className="skip-link">跳至图片工作区</a>
    <aside className={`sidebar ${sidebarOpen ? 'is-open' : ''}`} aria-label="相册导航">
      <NavLink className="brand" to="/" aria-label="HDRBridge 首页" onClick={closeSidebar}><span className="brand-symbol"><Icon name="bridge" /></span><span>HDRBridge</span></NavLink>
      <nav className="sidebar-section" aria-label="资料库">
        <span className="sidebar-label">资料库</span>
        <NavLink to={libraryPath} className={({ isActive }) => `album-link ${isActive ? 'active' : ''}`} onClick={closeSidebar}><Icon name="grid" /><span>全部图片</span></NavLink>
        <NavLink to={albumPath('uncategorized')} className={({ isActive }) => `album-link ${isActive ? 'active' : ''}`} onClick={closeSidebar}><Icon name="inbox" /><span>未分类</span></NavLink>
      </nav>
      <div className="sidebar-section albums-section"><div className="sidebar-section-heading"><span className="sidebar-label">我的相册</span><button className="icon-button small" aria-label="新建相册" onClick={() => { closeSidebar(); setOverlay({ kind: 'create' }) }}><Icon name="plus" /></button></div>
        <nav className="album-list" aria-label="我的相册">
          {albums.data?.filter(album => album.id !== 'uncategorized').map(album => <NavLink key={album.id} to={albumPath(album.id)} title={album.name} className={({ isActive }) => `album-link ${isActive ? 'active' : ''}`} onClick={closeSidebar}><Icon name="album" /><span>{album.name}</span></NavLink>)}
          {albums.isPending && <p className="sidebar-note"><Spinner />正在载入…</p>}
          {albums.data?.length === 1 && <p className="sidebar-note">用相册整理每一段光影。</p>}
          {albums.isError && <button className="text-button sidebar-retry" onClick={() => void albums.refetch()}><Icon name="retry" />重新加载相册</button>}
        </nav>
      </div>
      <div className="sidebar-bottom"><strong>图片资料库</strong><p>原图存档 · JXR 自动转 HEIC</p></div>
    </aside>
    {sidebarOpen && <button className="sidebar-scrim" aria-label="关闭相册导航" onClick={closeSidebar} />}
    <div className="workspace">
      <header className="topbar"><div className="topbar-leading"><button ref={sidebarButton} className="icon-button mobile-menu" aria-label="切换相册导航" aria-expanded={sidebarOpen} onClick={() => setSidebarOpen(!sidebarOpen)}><Icon name="menu" /></button><span>资料库</span><span className="breadcrumb-separator">/</span><span className="breadcrumb-current">{inLibrary ? '全部图片' : '相册'}</span></div><span className="topbar-caption">每一处细节，清晰如初。</span><span className="mobile-brand">HDRBridge</span></header>
      <Routes>
        <Route path="/" element={<Navigate to={albumPath('uncategorized')} replace />} />
        <Route path={libraryPath} element={workspace} />
        <Route path="/albums/:id" element={workspace} />
        <Route path="*" element={<main id="main" className="page-not-found"><Icon name="album" /><h1>这个页面不在资料库中</h1><NavLink className="button primary" to={albumPath('uncategorized')}>返回未分类</NavLink></main>} />
      </Routes>
    </div>
    {overlay?.kind === 'create' && <AlbumEditor onClose={() => setOverlay(null)} notify={setToast} />}
    {overlay?.kind === 'edit' && <AlbumEditor album={overlay.album} onClose={() => setOverlay(null)} notify={setToast} />}
    {overlay?.kind === 'delete' && <DeleteAlbum album={overlay.album} onClose={() => setOverlay(null)} notify={setToast} />}
    {uploads.panel}
    <div className={`toast ${toast ? 'visible' : ''}`} role="status">{toast && <><Icon name="check" /><span>{toast}</span></>}</div>
  </div>
}

type GalleryProps = {
  albums: Album[]; albumsLoading: boolean; albumsError: Error | null; refreshAlbums: () => void
  onOverlay: (overlay: Overlay) => void; onUpload: (files: File[], album: Album) => void; notify: (message: string) => void
}

function GalleryRoute(props: GalleryProps) {
  const { id } = useParams()
  return <Gallery key={id ?? libraryPath} albumID={id} {...props} />
}

function Gallery({ albumID, albums, albumsLoading, albumsError, refreshAlbums, onOverlay, onUpload, notify }: GalleryProps & { albumID?: string }) {
  const navigate = useNavigate()
  const library = albumID === undefined
  const album = albums.find(album => album.id === albumID)
  const uploadAlbum = library ? albums.find(album => album.id === 'uncategorized') : album
  const [search, setSearch] = useState('')
  const [cursors, setCursors] = useState<(string | null)[]>([null])
  const cursor = cursors[cursors.length - 1]
  const photos = useQuery({
    queryKey: [...(library ? libraryImagesKey : ['images', albumID]), search, cursor],
    queryFn: ({ signal }) => {
      const params = new URLSearchParams({ search })
      if (cursor) params.set('cursor', cursor)
      const path = library ? libraryPath : `${albumPath(albumID)}/images`
      return request<ImagePage>(`${path}?${params}`, { signal })
    },
  })
  const [detailID, setDetailID] = useState<string | null>(null)
  const [selecting, setSelecting] = useState(false)
  const [selection, setSelection] = useState<Map<string, BatchPhoto>>(() => new Map())
  const [batch, setBatch] = useState<PhotoAction['kind'] | null>(null)
  const [dragging, setDragging] = useState(false)
  const dragDepth = useRef(0)
  const input = useRef<HTMLInputElement>(null)
  const menu = useRef<HTMLDetailsElement>(null)
  const selectPageButton = useRef<HTMLButtonElement>(null)
  const notFound = !library && photos.error instanceof APIError && photos.error.status === 404
  const title = library ? '全部图片' : album?.name ?? (albumsLoading ? '正在载入…' : notFound ? '相册不存在' : '相册')
  useEffect(() => {
    document.title = `${title} — HDRBridge`
  }, [title])
  useEffect(() => {
    if (selecting) selectPageButton.current?.focus()
  }, [selecting])
  const images = photos.data?.images ?? []
  const total = photos.data?.total ?? 0
  const detailIndex = images.findIndex(photo => photo.id === detailID)
  const allOnPageSelected = images.length > 0 && images.every(photo => selection.has(photo.id))
  const destinations = albums.filter(album => album.id !== albumID)
  const albumNames = new Map(albums.map(album => [album.id, album.name]))

  useEffect(() => {
    if (cursor && photos.isSuccess && !photos.isFetching && photos.data.images.length === 0) {
      setCursors(previous => previous.slice(0, -1))
    }
  }, [cursor, photos.isSuccess, photos.isFetching, photos.data])

  function selectPhotos(items: BatchPhoto[], checked: boolean) {
    setSelection(previous => {
      const next = new Map(previous)
      for (const item of items) {
        if (checked) next.set(item.id, item)
        else next.delete(item.id)
      }
      return next
    })
  }
  function finishSelection() {
    setSelecting(false)
    setSelection(new Map())
  }
  function completeBatch(ids: string[]) {
    if (ids.length > 0 && (batch === 'delete' || !library)) setCursors([null])
    if (ids.length === selection.size) {
      finishSelection()
      return
    }
    setSelection(previous => {
      const next = new Map(previous)
      for (const id of ids) next.delete(id)
      return next
    })
  }
  function chooseFiles(files: File[]) { if (uploadAlbum && files.length) onUpload(files, uploadAlbum) }
  function filesDragged(event: DragEvent) { return event.dataTransfer.types.includes('Files') }
  function enter(event: DragEvent) {
    if (!filesDragged(event)) return
    event.preventDefault(); dragDepth.current++; if (uploadAlbum) setDragging(true)
  }
  function leave(event: DragEvent) {
    if (!filesDragged(event)) return
    event.preventDefault(); dragDepth.current = Math.max(0, dragDepth.current - 1)
    if (dragDepth.current === 0) setDragging(false)
  }
  function drop(event: DragEvent) {
    if (!filesDragged(event)) return
    event.preventDefault(); dragDepth.current = 0; setDragging(false)
    chooseFiles(Array.from(event.dataTransfer.files))
  }
  function openMenu(kind: 'edit' | 'delete') { if (menu.current) menu.current.open = false; if (album) onOverlay({ kind, album }) }
  return <main id="main" className={`album-workspace ${dragging ? 'dragging' : ''}`} onDragEnter={enter} onDragLeave={leave} onDragOver={event => { if (filesDragged(event)) { event.preventDefault(); event.dataTransfer.dropEffect = uploadAlbum ? 'copy' : 'none' } }} onDrop={drop}>
    <input ref={input} type="file" accept={acceptedImageFiles} multiple className="visually-hidden" tabIndex={-1} aria-label="选择图片" onChange={event => { chooseFiles(Array.from(event.target.files ?? [])); event.target.value = '' }} />
    <div className="album-heading"><div className="album-title"><span className="eyebrow">YOUR LIGHT, PRESERVED.</span><h1 tabIndex={-1}>{title}</h1><p>{library ? '浏览所有相册和未分类中的图片。' : album?.description || (albumID === 'uncategorized' ? '所有尚未归类的光影，都在这里。' : '收集此刻，保留每一处细节。')}</p></div><div className="album-actions">
      {album && albumID !== 'uncategorized' && <details ref={menu} className="album-menu" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false }} onKeyDown={event => { if (event.key === 'Escape' && menu.current) { menu.current.open = false; menu.current.querySelector('summary')?.focus() } }}><summary role="button" className="icon-button outlined" aria-label="相册操作"><Icon name="more" /></summary><div className="menu-content"><button onClick={() => openMenu('edit')}><Icon name="edit" />编辑相册</button><button className="destructive" onClick={() => openMenu('delete')}><Icon name="trash" />删除相册</button></div></details>}
      <button className="button primary" disabled={!uploadAlbum} onClick={() => input.current?.click()}><Icon name="plus" />{library ? '上传到未分类' : '上传图片'}</button>
    </div></div>
    {albumsError && <ErrorNotice message={`相册加载失败：${errorMessage(albumsError)}`} onRetry={refreshAlbums} />}
    {photos.isError && <ErrorNotice message={notFound ? '相册可能已被删除，请选择其他相册。' : `图片加载失败：${errorMessage(photos.error)}`} onRetry={() => void photos.refetch()} />}
    <div className="gallery-toolbar"><label className="search-field"><Icon name="search" /><input type="search" maxLength={512} placeholder="搜索图片名称" aria-label="搜索图片名称" value={search} onChange={event => { setSearch(event.target.value); setCursors([null]) }} /></label></div>
    {selecting && <div className="selection-toolbar" role="group" aria-label="图片多选操作">
      <div className="selection-summary"><strong role="status">已选 {selection.size} 张</strong><button ref={selectPageButton} className="text-button" disabled={!images.length} onClick={() => selectPhotos(images, !allOnPageSelected)}>{allOnPageSelected ? '取消本页全选' : '全选本页'}</button></div>
      <div className="selection-actions">
        <button className="button secondary" disabled={!selection.size || !destinations.length} onClick={() => setBatch('move')}><Icon name="move" />批量移动</button>
        <button className="button secondary destructive" disabled={!selection.size} onClick={() => setBatch('delete')}><Icon name="trash" />批量删除</button>
        <button className="text-button" onClick={finishSelection}>退出多选</button>
      </div>
    </div>}
    {photos.isPending ? <div className="gallery-loading" role="status"><Spinner /><p>正在整理你的图片…</p></div> : notFound ? <div className="empty-state"><Icon name="album" /><h2>这个相册已不在这里</h2><button className="button secondary" onClick={() => navigate(albumPath('uncategorized'))}>返回未分类</button></div> : total === 0 && !photos.isError ? <div className="empty-state">
      <div className="empty-image-mark"><Icon name="image" /><span><Icon name="plus" /></span></div><h2>添加第一张图片</h2><p>将图片拖放到这里，或一次选择多张。<br />常见格式保留原图，JXR 转为 HEIC 后完成上传。</p><button className="button secondary" onClick={() => input.current?.click()} disabled={!uploadAlbum}><Icon name="upload" />选择图片</button><span className="empty-support">JXR、JPEG、PNG、GIF、WebP、AVIF、BMP、ICO、SVG、TIFF、HEIC / HEIF、JPEG XL</span>
    </div> : images.length === 0 && !photos.isError && cursors.length === 1 ? <div className="empty-state compact"><Icon name="search" /><h2>没有符合条件的图片</h2><p>试试其他名称，或清除搜索。</p><button className="text-button" onClick={() => { setSearch(''); setCursors([null]) }}>清除搜索</button></div> : <>
      <div className="gallery-meta"><span>{search ? `${photos.data?.matched ?? 0} 张匹配图片` : `${total} 张图片`}</span><div className="gallery-meta-actions"><span>最新添加在前</span>{!selecting && images.length > 0 && <button className="button secondary" onClick={() => setSelecting(true)}><Icon name="check" />多选</button>}</div></div>
      <div className="photo-grid">{images.map(photo => {
        const checked = selection.has(photo.id)
        const albumName = albumNames.get(photo.album_id)
        return <button key={photo.id} className={`photo-card${selecting && checked ? ' is-selected' : ''}`} aria-pressed={selecting ? checked : undefined} aria-label={`${selecting ? '选择' : '查看'} ${photo.original_name}`} onClick={() => selecting ? selectPhotos([photo], !checked) : setDetailID(photo.id)}>
          <div className="photo-card-image">
            <PhotoPreview photo={photo} />
            <span className="photo-format">{isJXR(photo) ? convertedHEICLabel(photo) : photoFormat(photo)}</span>
            {selecting ? <span className="photo-check" aria-hidden="true">{checked && <Icon name="check" />}</span> : <span className="photo-open"><Icon name="plus" /></span>}
          </div>
          <div className="photo-card-caption">
            <h2 title={photo.original_name}>{photo.original_name}</h2>
            {library && albumName && <span className="photo-album" title={albumName}>{albumName}</span>}
          </div>
        </button>
      })}</div>
      {(cursors.length > 1 || photos.data?.next_cursor) && <div className="gallery-pagination"><button className="button secondary" disabled={cursors.length === 1} onClick={() => setCursors(previous => previous.slice(0, -1))}>上一页</button><span>第 {cursors.length} 页</span><button className="button secondary" disabled={!photos.data?.next_cursor} onClick={() => { if (photos.data?.next_cursor) setCursors(previous => [...previous, photos.data!.next_cursor]) }}>下一页</button></div>}
    </>}
    {dragging && <div className="drop-overlay"><Icon name="upload" /><h2>松开即可上传</h2><p>添加到「{uploadAlbum?.name}」</p></div>}
    {detailID && <PhotoDetail key={detailID} id={detailID} albums={albums} onClose={() => setDetailID(null)} notify={notify} onPrevious={detailIndex > 0 ? () => setDetailID(images[detailIndex - 1].id) : undefined} onNext={detailIndex >= 0 && detailIndex < images.length - 1 ? () => setDetailID(images[detailIndex + 1].id) : undefined} />}
    {batch && <BatchPhotos kind={batch} photos={[...selection.values()]} destinations={destinations} onCompleted={completeBatch} onClose={() => setBatch(null)} notify={notify} />}
  </main>
}

'use strict';

class CameraPlayer {
  constructor(cameraId, streamInfo, videoEl) {
    this.cameraId   = cameraId;
    this.streamInfo = streamInfo;
    this.videoEl    = videoEl;
    this.ms          = null;
    this.sb          = null;
    this.queue       = [];
    this.ws          = null;
    this.closed      = false;
    this.recordingId = null;
    this._initMSE();
  }

  _initMSE() {
    this.ms = new MediaSource();
    this.videoEl.src = URL.createObjectURL(this.ms);
    this.ms.addEventListener('sourceopen', () => this._onSourceOpen());
  }

  _onSourceOpen() {
    const codec = this.streamInfo.video_codec || '';
    let mimeType;
    if (codec.includes('265') || codec.includes('HEVC') || codec.includes('hevc')) {
      mimeType = 'video/mp4; codecs="hvc1.1.6.L93.B0"';
    } else {
      mimeType = 'video/mp4; codecs="avc1.42E01E"';
    }
    if (!MediaSource.isTypeSupported(mimeType)) {
      mimeType = 'video/mp4';
    }
    try {
      this.sb = this.ms.addSourceBuffer(mimeType);
      this.sb.addEventListener('updateend', () => this._flushQueue());
    } catch (e) {
      console.error('CameraPlayer: addSourceBuffer failed', e);
      this._setStatus('error');
    }
  }

  connect() {
    if (this.closed) return;
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const url = `${proto}://${location.host}/ws/stream/${this.cameraId}`;
    this.ws = new WebSocket(url);
    this.ws.binaryType = 'arraybuffer';
    this.ws.onopen    = () => this._setStatus('active');
    this.ws.onmessage = (e) => this._onChunk(e.data);
    this.ws.onerror   = () => this._setStatus('error');
    this.ws.onclose   = () => {
      if (!this.closed) {
        this._setStatus('reconnecting');
        setTimeout(() => this.connect(), 3000);
      }
    };
  }

  _onChunk(data) {
    if (!this.sb) { this.queue.push(data); return; }
    if (this.sb.updating) { this.queue.push(data); return; }
    try {
      this.sb.appendBuffer(data);
    } catch (e) {
      if (e.name === 'QuotaExceededError') {
        this._trimBuffer();
        this.queue.push(data);
      }
    }
  }

  _flushQueue() {
    if (this.queue.length === 0 || !this.sb || this.sb.updating) return;
    try {
      this.sb.appendBuffer(this.queue.shift());
    } catch (e) {
      if (e.name === 'QuotaExceededError') this._trimBuffer();
    }
  }

  _trimBuffer() {
    try {
      const buf = this.sb.buffered;
      if (buf.length === 0) return;
      const end = buf.end(buf.length - 1);
      const start = buf.start(0);
      if (end - start > 10) {
        this.sb.remove(start, end - 5);
      }
    } catch (_) {}
  }

  _setStatus(status) {
    if (this.onStatus) this.onStatus(status);
  }

  async startRecord() {
    if (this.recordingId) return;
    const csrfToken = this._getCsrf();
    try {
      const resp = await fetch(`/api/record/start/${this.cameraId}`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          channel:  this.streamInfo.channel  || 1,
          sub_type: this.streamInfo.sub_type || 0,
        }),
      });
      if (!resp.ok) throw new Error(await resp.text());
      const data = await resp.json();
      this.recordingId = data.recording_id;
      this._setStatus('active');
      return true;
    } catch (e) {
      console.error('startRecord:', e);
      return false;
    }
  }

  async stopRecord() {
    if (!this.recordingId) return;
    const csrfToken = this._getCsrf();
    try {
      await fetch('/api/record/stop', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({
          recording_id: this.recordingId,
          camera_key: `${this.streamInfo.ip}:${this.streamInfo.port}`,
        }),
      });
    } catch (e) {
      console.error('stopRecord:', e);
    } finally {
      this.recordingId = null;
    }
  }

  destroy() {
    this.closed = true;
    if (this.ws) { this.ws.onclose = null; this.ws.close(); }
    if (this.ms && this.ms.readyState === 'open') {
      try { this.ms.endOfStream(); } catch (_) {}
    }
    this.videoEl.src = '';
  }

  _getCsrf() {
    return document.cookie.split(';')
      .find(c => c.trim().startsWith('csrf_token='))
      ?.split('=')[1] || '';
  }
}

const StreamApp = (() => {
  let layout        = 1;
  let cells         = [];
  let cameras       = [];
  let selectedCamId = null;
  let availStreams   = [];
  let recPanelOpen  = false;

  function init() {
    buildGrid(layout);
    loadCameras();
    bindLayoutButtons();
    bindRecPanel();
    document.getElementById('open-stream-btn').addEventListener('click', openSelectedStream);
  }

  function buildGrid(n) {
    layout = n;
    const grid = document.getElementById('grid');
    grid.className = `layout-${n}`;
    cells.forEach(cell => { if (cell.player) cell.player.destroy(); });
    cells = [];
    grid.innerHTML = '';
    for (let i = 0; i < n; i++) {
      const cellEl = document.createElement('div');
      cellEl.className = 'cell';
      cellEl.innerHTML = `
        <div class="cell-header">
          <span class="cell-title">Cell ${i + 1}</span>
          <span class="cell-status">idle</span>
        </div>
        <div class="cell-video-wrap">
          <video autoplay muted playsinline></video>
          <div class="cell-placeholder">Select a camera from the sidebar</div>
        </div>
        <div class="cell-footer">
          <button class="cell-btn" data-action="record" disabled>&#9210; Record</button>
          <button class="cell-btn danger" data-action="close" disabled>Close</button>
          <span class="rec-size"></span>
        </div>`;
      grid.appendChild(cellEl);
      cells.push({ player: null, cameraId: null, streamInfo: null, el: cellEl, index: i });
      bindCellButtons(cells[i]);
    }
  }

  function bindLayoutButtons() {
    document.querySelectorAll('.layout-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('.layout-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        buildGrid(parseInt(btn.dataset.layout, 10));
      });
    });
  }

  function bindCellButtons(cell) {
    cell.el.querySelector('[data-action="record"]').addEventListener('click', async () => {
      const btn = cell.el.querySelector('[data-action="record"]');
      if (!cell.player) return;
      if (cell.player.recordingId) {
        await cell.player.stopRecord();
        btn.textContent = '&#9210; Record';
        btn.classList.remove('record-on');
        cell.el.querySelector('.rec-size').textContent = '';
        loadRecordings();
      } else {
        const ok = await cell.player.startRecord();
        if (ok) {
          btn.classList.add('record-on');
          btn.textContent = 'Stop';
        }
      }
    });
    cell.el.querySelector('[data-action="close"]').addEventListener('click', () => {
      closeCell(cell);
    });
  }

  async function loadCameras() {
    try {
      const resp = await fetch('/cams', { credentials: 'include' });
      if (!resp.ok) return;
      cameras = await resp.json();
      renderCamList();
    } catch (e) {
      console.error('loadCameras:', e);
    }
  }

  function renderCamList() {
    const list = document.getElementById('cam-list');
    list.innerHTML = '';
    cameras.forEach(cam => {
      if (!cam.IsDefined) return;
      const item = document.createElement('div');
      item.className = 'cam-item';
      item.dataset.id = cam.ID;
      const nameSpan = document.createElement('span');
      nameSpan.className = 'cam-item-name';
      nameSpan.textContent = cam.Name || cam.IP;
      const ipSpan = document.createElement('span');
      ipSpan.className = 'cam-item-ip';
      ipSpan.textContent = `${cam.IP}:${cam.Port}`;
      item.appendChild(nameSpan);
      item.appendChild(ipSpan);
      item.addEventListener('click', () => selectCamera(cam));
      list.appendChild(item);
    });
  }

  async function selectCamera(cam) {
    document.querySelectorAll('.cam-item').forEach(el => el.classList.remove('selected'));
    document.querySelector(`.cam-item[data-id="${cam.ID}"]`)?.classList.add('selected');
    selectedCamId = cam.ID;

    const panel = document.getElementById('stream-select-panel');
    const sel   = document.getElementById('stream-select');
    const btn   = document.getElementById('open-stream-btn');
    panel.classList.add('visible');
    sel.innerHTML = '<option disabled selected>Loading streams\u2026</option>';
    btn.disabled = true;
    availStreams = [];

    try {
      const resp = await fetch(`/api/stream/channels/${cam.ID}`, { credentials: 'include' });
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
      availStreams = await resp.json();
      if (!availStreams || availStreams.length === 0) {
        sel.innerHTML = '<option disabled selected>No streams found</option>';
        return;
      }
      sel.innerHTML = availStreams.map((s, i) =>
        `<option value="${i}">${s.label}${s.resolution ? ' \u2014 ' + s.resolution : ''}${s.video_codec ? ' (' + s.video_codec + ')' : ''}</option>`
      ).join('');
      btn.disabled = false;
    } catch (e) {
      console.error('stream discovery:', e);
      sel.innerHTML = '<option disabled selected>Discovery failed</option>';
      toast('Failed to get stream list: ' + e.message);
    }
  }

  function openSelectedStream() {
    if (!selectedCamId || availStreams.length === 0) return;
    const sel = document.getElementById('stream-select');
    const idx = parseInt(sel.value, 10);
    const streamInfo = availStreams[idx];
    if (!streamInfo) return;
    const target = cells.find(c => !c.player) || cells[0];
    openStreamInCell(target, selectedCamId, streamInfo);
  }

  function openStreamInCell(cell, cameraId, streamInfo) {
    if (cell.player) closeCell(cell);
    const cam = cameras.find(c => c.ID === cameraId);
    const title = (cam && (cam.Name || cam.IP) || 'Camera') + ' \u2014 ' + streamInfo.label;
    cell.el.querySelector('.cell-title').textContent = title;
    cell.el.querySelector('.cell-placeholder').style.display = 'none';

    const videoEl = cell.el.querySelector('video');
    const player = new CameraPlayer(cameraId, streamInfo, videoEl);
    player.onStatus = (status) => {
      const statusEl = cell.el.querySelector('.cell-status');
      statusEl.textContent = status;
      statusEl.className = 'cell-status ' + status;
    };
    cell.player     = player;
    cell.cameraId   = cameraId;
    cell.streamInfo = streamInfo;
    cell.el.querySelector('[data-action="record"]').disabled = false;
    cell.el.querySelector('[data-action="close"]').disabled  = false;
    player.connect();
  }

  function closeCell(cell) {
    if (!cell.player) return;
    if (cell.player.recordingId) cell.player.stopRecord();
    cell.player.destroy();
    cell.player     = null;
    cell.cameraId   = null;
    cell.streamInfo = null;
    cell.el.querySelector('.cell-title').textContent = `Cell ${cell.index + 1}`;
    cell.el.querySelector('.cell-status').textContent = 'idle';
    cell.el.querySelector('.cell-status').className  = 'cell-status';
    cell.el.querySelector('.cell-placeholder').style.display = '';
    cell.el.querySelector('[data-action="record"]').disabled = true;
    cell.el.querySelector('[data-action="close"]').disabled  = true;
    cell.el.querySelector('[data-action="record"]').textContent = '&#9210; Record';
    cell.el.querySelector('[data-action="record"]').classList.remove('record-on');
    cell.el.querySelector('.rec-size').textContent = '';
  }

  function bindRecPanel() {
    document.getElementById('rec-toggle').addEventListener('click', toggleRecPanel);
    document.getElementById('rec-panel-close').addEventListener('click', toggleRecPanel);
  }

  function toggleRecPanel() {
    recPanelOpen = !recPanelOpen;
    document.getElementById('rec-panel').classList.toggle('open', recPanelOpen);
    if (recPanelOpen) loadRecordings();
  }

  async function loadRecordings() {
    const list = document.getElementById('rec-list');
    try {
      const resp = await fetch('/api/record/list', { credentials: 'include' });
      if (!resp.ok) { list.textContent = 'Error loading recordings.'; return; }
      const recs = await resp.json();
      list.innerHTML = '';
      if (!recs || recs.length === 0) {
        const p = document.createElement('p');
        p.style.cssText = 'color:#555;padding:12px;font-size:13px;';
        p.textContent = 'No recordings yet.';
        list.appendChild(p);
        return;
      }
      recs.forEach(r => {
        const isActive = r.status === 'active';
        const item = document.createElement('div');
        item.className = 'rec-item' + (isActive ? ' active' : '');

        const camDiv = document.createElement('div');
        camDiv.className = 'rec-item-cam';
        camDiv.textContent = `${r.camera_key} \u2014 ch${r.channel}/${r.sub_type === 0 ? 'main' : 'sub'}`;

        const timeDiv = document.createElement('div');
        timeDiv.className = 'rec-item-time';
        const started = new Date(r.started_at).toLocaleString();
        const size = r.size_bytes ? formatBytes(r.size_bytes) : '';
        timeDiv.textContent = started + (size ? ' \u00b7 ' + size : '') + (isActive ? ' \u00b7 recording\u2026' : '');

        const actionsDiv = document.createElement('div');
        actionsDiv.className = 'rec-item-actions';
        if (!isActive) {
          const dlLink = document.createElement('a');
          dlLink.href = `/api/record/download/${encodeURIComponent(r.id)}`;
          dlLink.download = '';
          dlLink.textContent = 'Download';
          actionsDiv.appendChild(dlLink);
        }
        if (window.SAMAR?.isModer && !isActive) {
          const delBtn = document.createElement('button');
          delBtn.textContent = 'Delete';
          delBtn.addEventListener('click', () => _deleteRec(r.id));
          actionsDiv.appendChild(delBtn);
        }

        item.appendChild(camDiv);
        item.appendChild(timeDiv);
        item.appendChild(actionsDiv);
        list.appendChild(item);
      });
    } catch (e) {
      list.textContent = 'Error: ' + e.message;
    }
  }

  async function _deleteRec(id) {
    const csrfToken = document.cookie.split(';')
      .find(c => c.trim().startsWith('csrf_token='))?.split('=')[1] || '';
    try {
      await fetch(`/api/record/${id}`, {
        method: 'DELETE',
        credentials: 'include',
        headers: { 'X-CSRF-Token': csrfToken },
      });
      loadRecordings();
    } catch (e) {
      toast('Delete failed: ' + e.message);
    }
  }

  function formatBytes(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
    return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB';
  }

  let toastTimer = null;
  function toast(msg) {
    const el = document.getElementById('toast');
    el.textContent = msg;
    el.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.remove('show'), 3500);
  }

  return { init };
})();

<script>
  import { onMount, onDestroy } from 'svelte'
  import {
    SelectDirectory,
    ResizeDir,
    CancelResize,
    DefaultOptions,
  } from './lib/wailsjs/go/main/App.js'
  import { EventsOn, EventsOnce } from './lib/wailsjs/runtime/runtime.js'

  // ---- state ----------------------------------------------------------------
  let inputDir = ''
  let processorName = ''
  let processorIsGPU = false
  let opts = {
    TargetWidth: 1920,
    TargetHeight: 1080,
    PreserveAspect: true,
    Algorithm: 'lanczos',
    JPEGQuality: 85,
    OutputDir: '',
    MaxConcurrency: 0,
  }

  // batch progress
  let running = false
  let done = 0
  let total = 0
  let current = ''
  let errors = []
  let finished = false
  let unlisten = null
  let startTime = null
  let elapsed = 0
  let ticker = null

  // preset selection
  let activePreset = '1920'
  const presets = [
    { id: 'instagram', label: 'Instagram', w: 1080, h: 1080 },
    { id: '1920',      label: 'Full HD',   w: 1920, h: 1080 },
    { id: '2560',      label: '2K',        w: 2560, h: 1440 },
    { id: 'custom',    label: 'Własne',    w: null,  h: null },
  ]

  function applyPreset(p) {
    activePreset = p.id
    if (p.w !== null) {
      opts.TargetWidth  = p.w
      opts.TargetHeight = p.h
    }
  }

  // quality levels
  let qualityLevel = 'high'
  const qualityMap = { web: 75, high: 85, max: 95 }
  $: opts.JPEGQuality = qualityMap[qualityLevel] || 85

  // drag-over state
  let dragOver = false

  // ---- lifecycle ------------------------------------------------------------
  onMount(async () => {
    // Wait for the backend to finish GPU/OpenCL initialisation before reading
    // the processor name. Go emits "app:ready" with the name as payload once
    // startup() completes, so we never race with the ~0.6 s OpenCL init.
    EventsOnce('app:ready', (name) => {
      console.log('[gpu-resize] app:ready received, processor:', name)
      processorName = name
      processorIsGPU = name.toLowerCase().includes('gpu')
    })

    const defaults = await DefaultOptions()
    opts = {
      TargetWidth:    defaults.TargetWidth,
      TargetHeight:   defaults.TargetHeight,
      PreserveAspect: defaults.PreserveAspect,
      Algorithm:      defaults.Algorithm,
      JPEGQuality:    defaults.JPEGQuality,
      OutputDir:      defaults.OutputDir || '',
      MaxConcurrency: defaults.MaxConcurrency,
    }

    unlisten = EventsOn('resize:progress', (payload) => {
      console.log('[gpu-resize] resize:progress received:', JSON.stringify(payload))
      if (payload.finished) {
        running = false
        finished = true
        clearInterval(ticker)
        return
      }
      done    = payload.done
      total   = payload.total
      current = payload.current
      if (payload.error) {
        errors = [...errors, { file: payload.current, msg: payload.error }]
      }
    })
  })

  onDestroy(() => {
    if (unlisten) unlisten()
    clearInterval(ticker)
  })

  // ---- handlers -------------------------------------------------------------
  async function pickDirectory() {
    const dir = await SelectDirectory()
    if (dir) { inputDir = dir; finished = false; errors = [] }
  }

  // Wails doesn't support real FS drag-drop of folders, so we swallow the
  // event visually and show a pointer cursor but open the picker on drop.
  function onDragOver(e) { e.preventDefault(); dragOver = true }
  function onDragLeave()  { dragOver = false }
  async function onDrop(e) {
    e.preventDefault()
    dragOver = false
    await pickDirectory()
  }

  async function startResize() {
    if (!inputDir) return
    errors   = []
    done     = 0
    total    = 0
    current  = ''
    finished = false
    running  = true
    startTime = Date.now()
    elapsed  = 0
    ticker = setInterval(() => { elapsed = (Date.now() - startTime) / 1000 }, 200)

    const o = {
      ...opts,
      TargetWidth:  Math.max(1, parseInt(opts.TargetWidth)  || 1920),
      TargetHeight: Math.max(1, parseInt(opts.TargetHeight) || 1080),
      JPEGQuality:  Math.min(100, Math.max(1, parseInt(opts.JPEGQuality) || 85)),
    }

    try {
      await ResizeDir(inputDir, o)
    } catch (e) {
      running = false
      clearInterval(ticker)
      errors = [{ file: '', msg: String(e) }]
    }
  }

  async function cancel() {
    await CancelResize()
    running = false
    clearInterval(ticker)
  }

  function reset() {
    finished = false
    errors   = []
    done     = 0
    total    = 0
  }

  // ---- derived --------------------------------------------------------------
  $: progress   = total > 0 ? (done / total) * 100 : 0
  $: fps        = elapsed > 0.5 ? (done / elapsed).toFixed(1) : '…'
  $: dirName    = inputDir ? inputDir.split(/[\\/]/).filter(Boolean).pop() : ''
</script>

<style>
  /* ── CSS custom properties ─────────────────────────────────────────────── */
  :global(:root) {
    --bg:          #f5f5f7;
    --bg2:         #ffffff;
    --bg3:         #ebebeb;
    --border:      #d1d1d6;
    --text:        #1d1d1f;
    --text2:       #6e6e73;
    --text3:       #aeaeb2;
    --accent:      #0071e3;
    --accent-h:    #0077ed;
    --accent-dim:  #e8f0fd;
    --green:       #34c759;
    --green-dim:   #e3f9e8;
    --red:         #ff3b30;
    --red-dim:     #ffeeed;
    --shadow:      0 1px 3px rgba(0,0,0,.08), 0 4px 12px rgba(0,0,0,.06);
    --radius:      12px;
    --radius-sm:   8px;
  }

  @media (prefers-color-scheme: dark) {
    :global(:root) {
      --bg:         #1c1c1e;
      --bg2:        #2c2c2e;
      --bg3:        #3a3a3c;
      --border:     #3a3a3c;
      --text:       #f5f5f7;
      --text2:      #aeaeb2;
      --text3:      #636366;
      --accent:     #0a84ff;
      --accent-h:   #409cff;
      --accent-dim: #0a2a4a;
      --green:      #30d158;
      --green-dim:  #0d2e18;
      --red:        #ff453a;
      --red-dim:    #3a0e0c;
      --shadow:     0 1px 3px rgba(0,0,0,.4), 0 4px 12px rgba(0,0,0,.3);
    }
  }

  :global(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(body) {
    font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', 'Segoe UI', sans-serif;
    background: var(--bg);
    color: var(--text);
    height: 100vh;
    overflow: hidden;
    -webkit-font-smoothing: antialiased;
  }

  /* ── Shell layout ──────────────────────────────────────────────────────── */
  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    padding: 20px 24px 20px;
    gap: 14px;
    max-width: 680px;
    margin: 0 auto;
  }

  /* ── Header ─────────────────────────────────────────────────────────────── */
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .app-title {
    font-size: 1.1rem;
    font-weight: 700;
    letter-spacing: -0.03em;
    color: var(--text);
  }

  .gpu-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 0.7rem;
    font-weight: 600;
    padding: 3px 10px;
    border-radius: 999px;
    background: var(--accent-dim);
    color: var(--accent);
    letter-spacing: 0.02em;
  }
  .gpu-pill .dot {
    width: 6px; height: 6px;
    border-radius: 50%;
    background: var(--green);
  }

  /* ── Drop zone ───────────────────────────────────────────────────────────── */
  .dropzone {
    border: 2px dashed var(--border);
    border-radius: var(--radius);
    background: var(--bg2);
    padding: 28px 20px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    transition: border-color 0.15s, background 0.15s;
    text-align: center;
  }
  .dropzone:hover, .dropzone.over {
    border-color: var(--accent);
    background: var(--accent-dim);
  }
  .dropzone.has-dir {
    padding: 16px 20px;
    flex-direction: row;
    text-align: left;
    gap: 12px;
  }

  .drop-icon {
    font-size: 2rem;
    line-height: 1;
    user-select: none;
  }
  .dropzone.has-dir .drop-icon { font-size: 1.4rem; }

  .drop-label {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text);
  }
  .drop-hint {
    font-size: 0.75rem;
    color: var(--text2);
  }
  .drop-path {
    flex: 1;
    font-size: 0.8rem;
    color: var(--text2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .drop-dirname {
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .change-btn {
    flex-shrink: 0;
    font-size: 0.75rem;
    color: var(--accent);
    cursor: pointer;
    font-weight: 500;
    white-space: nowrap;
  }

  /* ── Card ────────────────────────────────────────────────────────────────── */
  .card {
    background: var(--bg2);
    border-radius: var(--radius);
    padding: 16px 18px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .section-title {
    font-size: 0.7rem;
    font-weight: 600;
    color: var(--text2);
    text-transform: uppercase;
    letter-spacing: 0.07em;
  }

  /* ── Presets ─────────────────────────────────────────────────────────────── */
  .preset-row {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }

  .preset-btn {
    background: var(--bg3);
    border: 1.5px solid transparent;
    border-radius: var(--radius-sm);
    padding: 8px 6px;
    cursor: pointer;
    text-align: center;
    transition: all 0.12s;
    user-select: none;
  }
  .preset-btn:hover:not(:disabled) {
    border-color: var(--accent);
    background: var(--accent-dim);
  }
  .preset-btn.active {
    border-color: var(--accent);
    background: var(--accent-dim);
    color: var(--accent);
  }
  .preset-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .preset-name {
    font-size: 0.8rem;
    font-weight: 600;
    color: inherit;
    display: block;
  }
  .preset-dim {
    font-size: 0.65rem;
    color: var(--text3);
    display: block;
    margin-top: 2px;
  }
  .preset-btn.active .preset-dim { color: var(--accent); opacity: 0.7; }

  .custom-dims {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    margin-top: 2px;
  }

  /* ── Quality pills ───────────────────────────────────────────────────────── */
  .quality-row {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }
  .quality-btn {
    background: var(--bg3);
    border: 1.5px solid transparent;
    border-radius: var(--radius-sm);
    padding: 7px 6px;
    cursor: pointer;
    text-align: center;
    transition: all 0.12s;
    user-select: none;
  }
  .quality-btn:hover:not(:disabled) {
    border-color: var(--accent);
    background: var(--accent-dim);
  }
  .quality-btn.active {
    border-color: var(--accent);
    background: var(--accent-dim);
    color: var(--accent);
  }
  .quality-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .quality-name { font-size: 0.8rem; font-weight: 600; display: block; color: inherit; }
  .quality-val  { font-size: 0.65rem; color: var(--text3); display: block; margin-top: 1px; }
  .quality-btn.active .quality-val { color: var(--accent); opacity: 0.7; }

  /* ── Options row ─────────────────────────────────────────────────────────── */
  .options-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }

  .toggle-row {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    user-select: none;
  }
  .toggle {
    position: relative;
    width: 32px;
    height: 18px;
    flex-shrink: 0;
  }
  .toggle input { display: none; }
  .toggle-track {
    position: absolute;
    inset: 0;
    background: var(--bg3);
    border-radius: 999px;
    transition: background 0.2s;
  }
  .toggle input:checked ~ .toggle-track { background: var(--accent); }
  .toggle-thumb {
    position: absolute;
    top: 2px; left: 2px;
    width: 14px; height: 14px;
    background: #fff;
    border-radius: 50%;
    transition: transform 0.2s;
    box-shadow: 0 1px 3px rgba(0,0,0,.2);
  }
  .toggle input:checked ~ .toggle-thumb { transform: translateX(14px); }
  .toggle-label { font-size: 0.8rem; color: var(--text2); }

  .algo-select {
    background: var(--bg3);
    border: 1.5px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 5px 10px;
    font-size: 0.8rem;
    color: var(--text);
    cursor: pointer;
    appearance: none;
    padding-right: 24px;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6'%3E%3Cpath d='M0 0l5 6 5-6z' fill='%23888'/%3E%3C/svg%3E");
    background-repeat: no-repeat;
    background-position: right 8px center;
  }
  .algo-select:focus { outline: none; border-color: var(--accent); }
  .algo-select:disabled { opacity: 0.4; cursor: not-allowed; }

  /* ── Input fields ────────────────────────────────────────────────────────── */
  input[type="number"] {
    background: var(--bg3);
    border: 1.5px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 7px 10px;
    font-size: 0.8rem;
    color: var(--text);
    width: 100%;
  }
  input:focus { outline: none; border-color: var(--accent); }
  input:disabled { opacity: 0.4; cursor: not-allowed; }
  input::placeholder { color: var(--text3); }

  .field { display: flex; flex-direction: column; gap: 4px; }
  .field-label {
    font-size: 0.7rem;
    font-weight: 500;
    color: var(--text2);
  }

  /* ── Primary action ──────────────────────────────────────────────────────── */
  .action-row {
    display: flex;
    gap: 8px;
  }

  .btn-primary {
    flex: 1;
    background: var(--accent);
    color: #fff;
    border: none;
    border-radius: var(--radius-sm);
    padding: 11px 16px;
    font-size: 0.9rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.12s, opacity 0.12s;
    letter-spacing: -0.01em;
  }
  .btn-primary:hover:not(:disabled) { background: var(--accent-h); }
  .btn-primary:disabled { opacity: 0.35; cursor: not-allowed; }

  .btn-cancel {
    background: var(--red-dim);
    color: var(--red);
    border: none;
    border-radius: var(--radius-sm);
    padding: 11px 16px;
    font-size: 0.875rem;
    font-weight: 600;
    cursor: pointer;
    transition: opacity 0.12s;
    white-space: nowrap;
  }
  .btn-cancel:hover { opacity: 0.8; }

  /* ── Progress card ───────────────────────────────────────────────────────── */
  .progress-card {
    background: var(--bg2);
    border-radius: var(--radius);
    padding: 16px 18px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .progress-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .progress-count {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text);
  }
  .progress-speed {
    font-size: 0.75rem;
    color: var(--text2);
  }

  .bar-outer {
    background: var(--bg3);
    border-radius: 999px;
    height: 6px;
    overflow: hidden;
  }
  .bar-inner {
    height: 100%;
    border-radius: 999px;
    background: var(--accent);
    transition: width 0.25s ease;
  }
  .bar-inner.complete { background: var(--green); }

  .current-file {
    font-size: 0.72rem;
    color: var(--text3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── Success / error states ──────────────────────────────────────────────── */
  .done-banner {
    display: flex;
    align-items: center;
    gap: 10px;
    background: var(--green-dim);
    border-radius: var(--radius-sm);
    padding: 10px 14px;
  }
  .done-icon { font-size: 1.2rem; }
  .done-text { font-size: 0.875rem; font-weight: 600; color: var(--green); }
  .done-sub  { font-size: 0.75rem; color: var(--text2); margin-top: 1px; }

  .error-banner {
    background: var(--red-dim);
    border-radius: var(--radius-sm);
    padding: 10px 14px;
  }
  .error-title { font-size: 0.8rem; font-weight: 600; color: var(--red); margin-bottom: 4px; }
  .error-item  { font-size: 0.72rem; color: var(--text2); margin-top: 3px; }

  .btn-ghost {
    background: none;
    border: 1.5px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 5px 12px;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--text2);
    cursor: pointer;
    margin-top: 8px;
  }
  .btn-ghost:hover { border-color: var(--accent); color: var(--accent); }
</style>

<div class="shell">

  <!-- ── Header ─────────────────────────────────────────────────────────── -->
  <div class="header">
    <span class="app-title">GPU Resizer</span>
    {#if processorName}
      <span class="gpu-pill">
        <span class="dot"></span>
        {processorIsGPU ? processorName.replace('GPU/OpenCL (','').replace(')','') : processorName}
      </span>
    {/if}
  </div>

  <!-- ── Drop zone ──────────────────────────────────────────────────────── -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="dropzone"
    class:has-dir={!!inputDir}
    class:over={dragOver}
    on:click={!running ? pickDirectory : undefined}
    on:dragover={onDragOver}
    on:dragleave={onDragLeave}
    on:drop={onDrop}
  >
    {#if !inputDir}
      <div class="drop-icon">📁</div>
      <div class="drop-label">Upuść folder lub kliknij, aby wybrać</div>
      <div class="drop-hint">Obsługiwane formaty: JPEG, PNG, BMP</div>
    {:else}
      <div class="drop-icon">🗂️</div>
      <div style="flex:1;overflow:hidden">
        <div class="drop-dirname">{dirName}</div>
        <div class="drop-path">{inputDir}</div>
      </div>
      {#if !running}
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <span class="change-btn" on:click|stopPropagation={pickDirectory}>Zmień</span>
      {/if}
    {/if}
  </div>

  <!-- ── Settings card ──────────────────────────────────────────────────── -->
  <div class="card">

    <!-- Presets -->
    <div>
      <div class="section-title" style="margin-bottom:8px">Rozmiar docelowy</div>
      <div class="preset-row">
        {#each presets as p}
          <button
            class="preset-btn"
            class:active={activePreset === p.id}
            disabled={running}
            on:click={() => applyPreset(p)}
          >
            <span class="preset-name">{p.label}</span>
            <span class="preset-dim">{p.w ? p.w + '×' + p.h : 'własne'}</span>
          </button>
        {/each}
      </div>
      {#if activePreset === 'custom'}
        <div class="custom-dims">
          <div class="field">
            <div class="field-label">Szerokość (px)</div>
            <input type="number" min="1" bind:value={opts.TargetWidth} disabled={running} />
          </div>
          <div class="field">
            <div class="field-label">Wysokość (px)</div>
            <input type="number" min="1" bind:value={opts.TargetHeight} disabled={running} />
          </div>
        </div>
      {/if}
    </div>

    <!-- Quality -->
    <div>
      <div class="section-title" style="margin-bottom:8px">Jakość JPEG</div>
      <div class="quality-row">
        <button class="quality-btn" class:active={qualityLevel==='web'}  disabled={running} on:click={() => qualityLevel='web'}>
          <span class="quality-name">Web</span>
          <span class="quality-val">75 · mały plik</span>
        </button>
        <button class="quality-btn" class:active={qualityLevel==='high'} disabled={running} on:click={() => qualityLevel='high'}>
          <span class="quality-name">Wysoka</span>
          <span class="quality-val">85 · balans</span>
        </button>
        <button class="quality-btn" class:active={qualityLevel==='max'}  disabled={running} on:click={() => qualityLevel='max'}>
          <span class="quality-name">Maksymalna</span>
          <span class="quality-val">95 · duży plik</span>
        </button>
      </div>
    </div>

    <!-- Options -->
    <div class="options-row">
      <label class="toggle-row">
        <span class="toggle">
          <input type="checkbox" bind:checked={opts.PreserveAspect} disabled={running} />
          <div class="toggle-track"></div>
          <div class="toggle-thumb"></div>
        </span>
        <span class="toggle-label">Zachowaj proporcje</span>
      </label>

      <select class="algo-select" bind:value={opts.Algorithm} disabled={running}>
        <option value="lanczos">Lanczos (ostra)</option>
        <option value="bilinear">Dwuliniowy (szybki)</option>
      </select>
    </div>

  </div>

  <!-- ── Action ─────────────────────────────────────────────────────────── -->
  <div class="action-row">
    <button
      class="btn-primary"
      disabled={running || !inputDir}
      on:click={startResize}
    >
      {running ? 'Przetwarzanie…' : 'Zmień rozmiar'}
    </button>
    {#if running}
      <button class="btn-cancel" on:click={cancel}>Anuluj</button>
    {/if}
  </div>

  <!-- ── Progress ───────────────────────────────────────────────────────── -->
  {#if running || (finished && !errors.length)}
    <div class="progress-card">
      {#if !finished}
        <div class="progress-header">
          <span class="progress-count">{done} / {total} zdjęć</span>
          <span class="progress-speed">{fps} zdjęć/s</span>
        </div>
        <div class="bar-outer">
          <div class="bar-inner" style="width:{progress}%"></div>
        </div>
        {#if current}
          <div class="current-file">{current.split(/[\\/]/).pop()}</div>
        {/if}
      {:else}
        <div class="done-banner">
          <span class="done-icon">✅</span>
          <div>
            <div class="done-text">Gotowe!</div>
            <div class="done-sub">{total} zdjęć przetworzone w {elapsed.toFixed(1)}s · zapisano w <em>resized/</em></div>
          </div>
        </div>
        <div class="bar-outer">
          <div class="bar-inner complete" style="width:100%"></div>
        </div>
        <button class="btn-ghost" on:click={reset}>Przetwórz kolejny folder</button>
      {/if}
    </div>
  {/if}

  <!-- ── Errors ─────────────────────────────────────────────────────────── -->
  {#if errors.length > 0}
    <div class="progress-card">
      <div class="error-banner">
        <div class="error-title">Błędy ({errors.length})</div>
        {#each errors as e}
          <div class="error-item">{e.file ? e.file.split(/[\\/]/).pop() + ': ' : ''}{e.msg}</div>
        {/each}
      </div>
    </div>
  {/if}

</div>

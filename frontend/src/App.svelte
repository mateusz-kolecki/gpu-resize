<script>
  import { onMount, onDestroy } from 'svelte'
  import {
    SelectDirectory,
    ResizeDir,
    CancelResize,
    ProcessorInfo,
    DefaultOptions,
  } from './lib/wailsjs/go/main/App.js'
  import { EventsOn } from './lib/wailsjs/runtime/runtime.js'

  // ---- state ----------------------------------------------------------------
  let inputDir = ''
  let processorName = ''
  let opts = {
    TargetWidth: 1920,
    TargetHeight: 1080,
    PreserveAspect: true,
    Algorithm: 'lanczos',
    JPEGQuality: 90,
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

  // ---- lifecycle ------------------------------------------------------------
  onMount(async () => {
    processorName = await ProcessorInfo()
    const defaults = await DefaultOptions()

    opts = {
      TargetWidth: defaults.TargetWidth,
      TargetHeight: defaults.TargetHeight,
      PreserveAspect: defaults.PreserveAspect,
      Algorithm: defaults.Algorithm,
      JPEGQuality: defaults.JPEGQuality,
      OutputDir: defaults.OutputDir || '',
      MaxConcurrency: defaults.MaxConcurrency,
    }

    unlisten = EventsOn('resize:progress', (payload) => {
      if (payload.finished) {
        running = false
        finished = true
        return
      }
      done = payload.done
      total = payload.total
      current = payload.current
      if (payload.error) {
        errors = [...errors, { file: payload.current, msg: payload.error }]
      }
    })
  })

  onDestroy(() => {
    if (unlisten) unlisten()
  })

  // ---- handlers -------------------------------------------------------------
  async function pickDirectory() {
    const dir = await SelectDirectory()
    if (dir) inputDir = dir
  }

  async function startResize() {
    if (!inputDir) return
    errors = []
    done = 0
    total = 0
    current = ''
    finished = false
    running = true

    // Clamp and cast
    const o = {
      ...opts,
      TargetWidth: Math.max(1, parseInt(opts.TargetWidth) || 1920),
      TargetHeight: Math.max(1, parseInt(opts.TargetHeight) || 1080),
      JPEGQuality: Math.min(100, Math.max(1, parseInt(opts.JPEGQuality) || 90)),
    }

    try {
      await ResizeDir(inputDir, o)
    } catch (e) {
      running = false
      errors = [{ file: '', msg: String(e) }]
    }
  }

  async function cancel() {
    await CancelResize()
    running = false
  }

  // Preserve aspect: auto-calculate height when width changes (and vice-versa
  // if only height is set) — just a UI affordance, the backend enforces it.
  function onAspectToggle() {
    opts.PreserveAspect = !opts.PreserveAspect
  }

  $: progress = total > 0 ? Math.round((done / total) * 100) : 0
</script>

<style>
  :global(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(body) {
    font-family: system-ui, -apple-system, sans-serif;
    background: #121212;
    color: #e0e0e0;
    height: 100vh;
    overflow: hidden;
  }

  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    padding: 24px;
    gap: 20px;
  }

  h1 {
    font-size: 1.25rem;
    font-weight: 600;
    color: #ffffff;
    letter-spacing: -0.02em;
  }

  .badge {
    display: inline-block;
    font-size: 0.7rem;
    padding: 2px 8px;
    border-radius: 999px;
    background: #1e3a5f;
    color: #7eb8f7;
    margin-left: 10px;
    vertical-align: middle;
    font-weight: 500;
  }

  /* Directory picker */
  .dir-row {
    display: flex;
    gap: 10px;
    align-items: center;
  }

  .dir-path {
    flex: 1;
    background: #1e1e1e;
    border: 1px solid #333;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 0.875rem;
    color: #aaa;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: default;
  }

  .dir-path.has-value { color: #e0e0e0; }

  button {
    background: #2563eb;
    color: #fff;
    border: none;
    border-radius: 6px;
    padding: 8px 16px;
    font-size: 0.875rem;
    cursor: pointer;
    font-weight: 500;
    transition: background 0.15s;
  }
  button:hover:not(:disabled) { background: #1d4ed8; }
  button:disabled { opacity: 0.4; cursor: not-allowed; }
  button.secondary { background: #374151; }
  button.secondary:hover:not(:disabled) { background: #4b5563; }
  button.danger { background: #b91c1c; }
  button.danger:hover:not(:disabled) { background: #991b1b; }

  /* Options grid */
  .options-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 14px 24px;
  }

  .field { display: flex; flex-direction: column; gap: 5px; }
  .field label {
    font-size: 0.75rem;
    font-weight: 500;
    color: #888;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  input[type="number"], select {
    background: #1e1e1e;
    border: 1px solid #333;
    border-radius: 6px;
    padding: 7px 10px;
    color: #e0e0e0;
    font-size: 0.875rem;
    width: 100%;
    appearance: none;
  }
  input[type="number"]:focus, select:focus {
    outline: none;
    border-color: #2563eb;
  }

  /* Aspect ratio toggle */
  .aspect-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .toggle {
    position: relative;
    width: 36px;
    height: 20px;
    cursor: pointer;
  }
  .toggle input { display: none; }
  .toggle-track {
    position: absolute;
    inset: 0;
    background: #333;
    border-radius: 999px;
    transition: background 0.2s;
  }
  .toggle input:checked ~ .toggle-track { background: #2563eb; }
  .toggle-thumb {
    position: absolute;
    top: 2px; left: 2px;
    width: 16px; height: 16px;
    background: #fff;
    border-radius: 50%;
    transition: transform 0.2s;
  }
  .toggle input:checked ~ .toggle-thumb { transform: translateX(16px); }

  /* Output dir override */
  .output-row input[type="text"] {
    background: #1e1e1e;
    border: 1px solid #333;
    border-radius: 6px;
    padding: 7px 10px;
    color: #e0e0e0;
    font-size: 0.875rem;
    width: 100%;
  }
  .output-row input::placeholder { color: #555; }
  .output-row input:focus { outline: none; border-color: #2563eb; }

  /* Action bar */
  .action-bar {
    display: flex;
    gap: 10px;
    align-items: center;
  }

  /* Progress */
  .progress-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .progress-bar-outer {
    background: #1e1e1e;
    border-radius: 999px;
    height: 6px;
    overflow: hidden;
  }
  .progress-bar-inner {
    height: 100%;
    background: #2563eb;
    border-radius: 999px;
    transition: width 0.2s;
  }

  .progress-label {
    font-size: 0.8rem;
    color: #888;
  }

  .current-file {
    font-size: 0.75rem;
    color: #555;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .finished-msg {
    font-size: 0.875rem;
    color: #4ade80;
    font-weight: 500;
  }

  /* Error log */
  .error-list {
    flex: 1;
    overflow-y: auto;
    background: #1a0e0e;
    border: 1px solid #4b1818;
    border-radius: 6px;
    padding: 10px 12px;
    font-size: 0.75rem;
    color: #f87171;
  }
  .error-list p { margin-bottom: 4px; }

  hr { border: none; border-top: 1px solid #222; }
</style>

<div class="shell">
  <div>
    <h1>Zmiana rozmiaru obrazów GPU <span class="badge">{processorName || '…'}</span></h1>
  </div>

  <!-- Wybór katalogu -->
  <div class="dir-row">
    <div class="dir-path" class:has-value={!!inputDir}>
      {inputDir || 'Nie wybrano katalogu'}
    </div>
    <button class="secondary" on:click={pickDirectory} disabled={running}>
      Przeglądaj…
    </button>
  </div>

  <hr />

  <!-- Opcje -->
  <div class="options-grid">
    <div class="field">
      <label for="opt-width">Maks. szerokość (px)</label>
      <input id="opt-width" type="number" min="1" bind:value={opts.TargetWidth} disabled={running} />
    </div>
    <div class="field">
      <label for="opt-height">Maks. wysokość (px)</label>
      <input id="opt-height" type="number" min="1" bind:value={opts.TargetHeight} disabled={running} />
    </div>
    <div class="field">
      <label for="opt-algo">Algorytm</label>
      <select id="opt-algo" bind:value={opts.Algorithm} disabled={running}>
        <option value="lanczos">Lanczos (wysoka jakość)</option>
        <option value="bilinear">Dwuliniowy (szybki)</option>
      </select>
    </div>
    <div class="field">
      <label for="opt-quality">Jakość JPEG (1–100)</label>
      <input id="opt-quality" type="number" min="1" max="100" bind:value={opts.JPEGQuality} disabled={running} />
    </div>
  </div>

  <div class="aspect-row">
    <label class="toggle" aria-label="Zachowaj proporcje">
      <input type="checkbox" bind:checked={opts.PreserveAspect} disabled={running} />
      <div class="toggle-track"></div>
      <div class="toggle-thumb"></div>
    </label>
    <span style="font-size:0.875rem">Zachowaj proporcje</span>
  </div>

  <div class="output-row">
    <input
      type="text"
      placeholder='Katalog wyjściowy (domyślnie: <źródło>/resized/)'
      bind:value={opts.OutputDir}
      disabled={running}
    />
  </div>

  <hr />

  <!-- Pasek akcji -->
  <div class="action-bar">
    <button on:click={startResize} disabled={running || !inputDir}>
      {running ? 'Przetwarzanie…' : 'Zmień rozmiar wszystkich obrazów'}
    </button>
    {#if running}
      <button class="danger" on:click={cancel}>Anuluj</button>
    {/if}
  </div>

  <!-- Postęp -->
  {#if running || finished}
    <div class="progress-area">
      <div class="progress-bar-outer">
        <div class="progress-bar-inner" style="width:{progress}%"></div>
      </div>
      <div class="progress-label">{done} / {total} obrazów {#if finished}— gotowe{/if}</div>
      {#if current && !finished}
        <div class="current-file">{current}</div>
      {/if}
      {#if finished && errors.length === 0}
        <div class="finished-msg">Wszystkie obrazy zostały pomyślnie przetworzone.</div>
      {:else if finished}
        <div class="finished-msg" style="color:#f87171">Zakończono z {errors.length} błędem(-ami).</div>
      {/if}
    </div>
  {/if}

  <!-- Błędy -->
  {#if errors.length > 0}
    <div class="error-list">
      {#each errors as e}
        <p>{e.file ? e.file + ': ' : ''}{e.msg}</p>
      {/each}
    </div>
  {/if}
</div>

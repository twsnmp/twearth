<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type { AIHardwareStatus, DownloadProgressEvent } from "../../../bindings/github.com/twsnmp/twearth/internal/app/models";
  import type { ModelInfo, PresetModelInfo } from "../../../bindings/github.com/twsnmp/twearth/internal/ai/model/models";
  import { t } from "../i18n/index.svelte";

  interface Props {
    onClose?: () => void;
    onModelsChanged?: () => void;
  }

  let { onClose, onModelsChanged }: Props = $props();

  let hardwareStatus = $state<AIHardwareStatus>({
    acceleration: "",
    detail: "",
    modelDir: "",
    libDir: "",
    wgpuLibPath: "",
    hasGpuLib: false,
  });
  let localModels = $state<ModelInfo[]>([]);
  let presets = $state<PresetModelInfo[]>([]);
  let selectedPreset = $state("");
  let customTarget = $state("");

  let isDownloadingModel = $state(false);
  let modelProgress = $state<DownloadProgressEvent>({
    downloaded: 0,
    total: 0,
    percent: 0,
    downloadedHuman: "0 B",
    totalHuman: "0 B",
  });

  let isSettingUpGPU = $state(false);
  let gpuProgress = $state<DownloadProgressEvent>({
    downloaded: 0,
    total: 0,
    percent: 0,
    downloadedHuman: "0 B",
    totalHuman: "0 B",
  });

  let errorMsg = $state("");
  let infoMsg = $state("");
  let confirmDelete = $state("");

  async function refreshData() {
    try {
      const [hw, models, presetList] = await Promise.all([
        AppService.GetAIHardwareStatus(),
        AppService.GetLocalModels(),
        AppService.GetModelPresets(),
      ]);
      hardwareStatus = hw;
      localModels = models ?? [];
      presets = presetList ?? [];
      if (presets.length > 0 && !selectedPreset) {
        selectedPreset = presets[0].name;
      }
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  onMount(() => {
    void refreshData();
    const offModel = Events.On("twearth:model-download-progress", (ev: any) => {
      if (ev?.data) {
        isDownloadingModel = true;
        modelProgress = ev.data;
      }
    });
    const offGpu = Events.On("twearth:gpu-download-progress", (ev: any) => {
      if (ev?.data) {
        isSettingUpGPU = true;
        gpuProgress = ev.data;
      }
    });
    return () => {
      if (typeof offModel === "function") offModel();
      if (typeof offGpu === "function") offGpu();
    };
  });

  async function downloadPreset() {
    if (!selectedPreset) return;
    await runModelDownload(selectedPreset);
  }

  async function downloadCustom() {
    const target = customTarget.trim();
    if (!target) return;
    const ok = await runModelDownload(target);
    if (ok) customTarget = "";
  }

  async function runModelDownload(target: string): Promise<boolean> {
    errorMsg = "";
    infoMsg = "";
    isDownloadingModel = true;
    modelProgress = { downloaded: 0, total: 0, percent: 0, downloadedHuman: "0 B", totalHuman: "0 B" };
    try {
      const err = await AppService.DownloadModel(target);
      if (err) {
        errorMsg = err;
        return false;
      }
      infoMsg = `${target} downloaded.`;
      await refreshData();
      onModelsChanged?.();
      return true;
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
      return false;
    } finally {
      isDownloadingModel = false;
    }
  }

  async function cancelModel() {
    await AppService.CancelModelDownload();
    isDownloadingModel = false;
  }

  async function deleteModel(name: string) {
    confirmDelete = "";
    errorMsg = "";
    infoMsg = "";
    const err = await AppService.DeleteLocalModel(name);
    if (err) {
      errorMsg = err;
      return;
    }
    infoMsg = `${name} deleted.`;
    await refreshData();
    onModelsChanged?.();
  }

  async function setupGPU() {
    errorMsg = "";
    infoMsg = "";
    isSettingUpGPU = true;
    gpuProgress = { downloaded: 0, total: 0, percent: 0, downloadedHuman: "0 B", totalHuman: "0 B" };
    try {
      const err = await AppService.DownloadGPULibrary();
      if (err) {
        errorMsg = err;
      } else {
        infoMsg = "WebGPU library installed.";
        await refreshData();
      }
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      isSettingUpGPU = false;
    }
  }

  async function cancelGPU() {
    await AppService.CancelGPUDownload();
    isSettingUpGPU = false;
  }
</script>

<div class="mm-card">
  <div class="mm-header">
    <h3>{t("ai.modelManagerTitle")}</h3>
    <button class="close-btn" type="button" onclick={() => onClose?.()} aria-label={t("app.close")}>✕</button>
  </div>

  <div class="mm-body">
    {#if errorMsg}
      <div class="banner err">
        <span>{errorMsg}</span>
        <button type="button" onclick={() => (errorMsg = "")}>✕</button>
      </div>
    {/if}
    {#if infoMsg}
      <div class="banner ok">
        <span>{infoMsg}</span>
        <button type="button" onclick={() => (infoMsg = "")}>✕</button>
      </div>
    {/if}

    <section class="box">
      <h4>{t("ai.gpuAcceleration")}</h4>
      <div class="kv-row">
        <span>{t("ai.activeBackend")}:</span>
        <span class="badge" class:gpu={hardwareStatus.acceleration === "GPU"}>
          {hardwareStatus.acceleration || "CPU"}
        </span>
        <span class="muted">{hardwareStatus.detail}</span>
      </div>
      <div class="kv-row">
        <span>{t("ai.wgpuLibrary")}:</span>
        {#if hardwareStatus.hasGpuLib}
          <span class="ok-text">{t("ai.gpuLibInstalled")}</span>
          <span class="muted path">{hardwareStatus.wgpuLibPath}</span>
        {:else}
          <span class="warn-text">{t("ai.gpuLibNotInstalled")}</span>
          {#if isSettingUpGPU}
            <div class="prog-wrap">
              <div class="prog-bar"><div class="prog-fill" style="width: {gpuProgress.percent}%"></div></div>
              <span class="muted">{gpuProgress.percent}% ({gpuProgress.downloadedHuman} / {gpuProgress.totalHuman})</span>
              <button class="btn-danger-sm" type="button" onclick={cancelGPU}>{t("app.cancel")}</button>
            </div>
          {:else}
            <button class="btn-primary-sm" type="button" onclick={setupGPU}>{t("ai.setupGPU")}</button>
          {/if}
        {/if}
      </div>
      <p class="hint">{t("ai.gpuHelp")}</p>
      <p class="hint">{t("ai.modelDirLabel")}: <code>{hardwareStatus.modelDir}</code></p>
    </section>

    <section class="box">
      <div class="box-head">
        <h4>{t("ai.localModelsTitle")}</h4>
        <span class="count-pill">{localModels.length}</span>
      </div>
      {#if localModels.length === 0}
        <p class="hint">{t("ai.noModels")}</p>
      {:else}
        <div class="model-list">
          {#each localModels as m (m.path)}
            <div class="model-row">
              <div class="m-info">
                <strong>{m.name}</strong>
                <span class="muted">{m.size_human}</span>
              </div>
              <div class="m-act">
                {#if confirmDelete === m.name}
                  <button class="btn-danger-sm" type="button" onclick={() => deleteModel(m.name)}>{t("ai.confirmDelete")}</button>
                  <button class="btn-sm" type="button" onclick={() => (confirmDelete = "")}>{t("app.cancel")}</button>
                {:else}
                  <button class="btn-danger-sm" type="button" onclick={() => (confirmDelete = m.name)}>{t("ai.deleteBtn")}</button>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </section>

    {#if isDownloadingModel}
      <section class="box prog-box">
        <div class="box-head">
          <span>{t("ai.downloading")} {modelProgress.percent}% ({modelProgress.downloadedHuman} / {modelProgress.totalHuman})</span>
          <button class="btn-danger-sm" type="button" onclick={cancelModel}>{t("app.cancel")}</button>
        </div>
        <div class="prog-bar"><div class="prog-fill" style="width: {modelProgress.percent}%"></div></div>
      </section>
    {/if}

    <section class="box">
      <h4>{t("ai.downloadPreset")}</h4>
      <div class="row-input">
        <select bind:value={selectedPreset} disabled={isDownloadingModel}>
          {#each presets as p (p.name)}
            <option value={p.name}>{p.name} ({p.size}) — {p.description}</option>
          {/each}
        </select>
        <button class="btn-primary-sm" type="button" disabled={isDownloadingModel || !selectedPreset} onclick={downloadPreset}>
          {t("ai.downloadBtn")}
        </button>
      </div>
    </section>

    <section class="box">
      <h4>{t("ai.downloadCustom")}</h4>
      <div class="row-input">
        <input type="text" bind:value={customTarget} placeholder={t("ai.customPlaceholder")} disabled={isDownloadingModel} />
        <button class="btn-sm" type="button" disabled={isDownloadingModel || !customTarget.trim()} onclick={downloadCustom}>
          {t("ai.downloadBtn")}
        </button>
      </div>
    </section>
  </div>

  <div class="mm-footer">
    <button class="btn-sm" type="button" onclick={() => onClose?.()}>{t("app.close")}</button>
  </div>
</div>

<style>
  .mm-card {
    background: #0d1222;
    border: 1px solid rgba(56, 189, 248, 0.35);
    border-radius: 12px;
    display: flex;
    flex-direction: column;
    max-height: 85vh;
    color: #e2e8f0;
    box-shadow: 0 20px 50px rgba(0, 0, 0, 0.65);
  }
  .mm-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.16);
  }
  .mm-header h3 {
    margin: 0;
    font-size: 0.98rem;
  }
  .close-btn {
    background: transparent;
    border: none;
    color: #94a3b8;
    font-size: 1rem;
    cursor: pointer;
  }
  .mm-body {
    padding: 14px 16px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .mm-footer {
    display: flex;
    justify-content: flex-end;
    padding: 10px 16px;
    border-top: 1px solid rgba(148, 163, 184, 0.16);
  }
  .box {
    background: rgba(15, 23, 42, 0.7);
    border: 1px solid rgba(148, 163, 184, 0.18);
    border-radius: 8px;
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .box h4 {
    margin: 0;
    font-size: 0.82rem;
    color: #38bdf8;
  }
  .box-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.8rem;
  }
  .kv-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    font-size: 0.78rem;
  }
  .badge {
    padding: 2px 7px;
    border-radius: 4px;
    background: rgba(148, 163, 184, 0.2);
    color: #cbd5e1;
    font-weight: 600;
    font-size: 0.72rem;
  }
  .badge.gpu {
    background: rgba(16, 185, 129, 0.2);
    color: #34d399;
  }
  .ok-text {
    color: #34d399;
    font-weight: 600;
  }
  .warn-text {
    color: #fbbf24;
    font-weight: 600;
  }
  .muted {
    color: #94a3b8;
    font-size: 0.74rem;
  }
  .path {
    max-width: 260px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    margin: 0;
    font-size: 0.73rem;
    color: #94a3b8;
  }
  .hint code {
    color: #7dd3fc;
  }
  .count-pill {
    font-size: 0.72rem;
    padding: 1px 7px;
    border-radius: 999px;
    background: rgba(56, 189, 248, 0.15);
    color: #38bdf8;
  }
  .model-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 180px;
    overflow-y: auto;
  }
  .model-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    background: rgba(2, 6, 23, 0.45);
    border-radius: 6px;
    font-size: 0.78rem;
  }
  .m-info {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }
  .m-info strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .m-act {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }
  .row-input {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .row-input select,
  .row-input input {
    flex: 1;
    min-width: 0;
    background: #0b1224;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    color: #e2e8f0;
    padding: 6px 8px;
    font-size: 0.78rem;
  }
  .prog-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 220px;
  }
  .prog-box {
    border-color: rgba(56, 189, 248, 0.45);
    background: rgba(14, 116, 144, 0.14);
  }
  .prog-bar {
    flex: 1;
    height: 7px;
    background: rgba(15, 23, 42, 0.9);
    border-radius: 999px;
    overflow: hidden;
  }
  .prog-fill {
    height: 100%;
    background: #38bdf8;
    transition: width 0.15s ease;
  }
  .banner {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 7px 10px;
    border-radius: 7px;
    font-size: 0.78rem;
  }
  .banner.err {
    background: rgba(239, 68, 68, 0.18);
    border: 1px solid rgba(239, 68, 68, 0.4);
    color: #fca5a5;
  }
  .banner.ok {
    background: rgba(16, 185, 129, 0.18);
    border: 1px solid rgba(16, 185, 129, 0.4);
    color: #6ee7b7;
  }
  .banner button {
    background: transparent;
    border: none;
    color: inherit;
    cursor: pointer;
  }
  .btn-sm,
  .btn-primary-sm,
  .btn-danger-sm {
    border-radius: 6px;
    padding: 5px 10px;
    font-size: 0.76rem;
    cursor: pointer;
    border: 1px solid rgba(148, 163, 184, 0.25);
    background: #131c31;
    color: #e2e8f0;
    white-space: nowrap;
  }
  .btn-primary-sm {
    background: rgba(14, 116, 144, 0.45);
    border-color: rgba(56, 189, 248, 0.6);
    color: #e0f2fe;
  }
  .btn-danger-sm {
    background: rgba(159, 18, 57, 0.35);
    border-color: rgba(244, 63, 94, 0.5);
    color: #fda4af;
  }
  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>

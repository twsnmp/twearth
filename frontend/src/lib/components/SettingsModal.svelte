<script lang="ts">
  import {
    appState,
    checkForUpdate,
    installUpdate,
    restartApp,
    saveAppConfig,
  } from "../stores/appState.svelte";
  import type { Config } from "../../../bindings/github.com/twsnmp/twearth/internal/store/models";

  let { inline = false, onClose = () => {} }: { inline?: boolean; onClose?: () => void } = $props();

  let draft: Config | null = $state(null);
  let saving = $state(false);
  let savedMessage = $state("");

  $effect(() => {
    if (appState.config && !draft) {
      draft = structuredClone($state.snapshot(appState.config));
    }
  });

  async function handleSave() {
    if (!draft) return;
    saving = true;
    savedMessage = "";
    try {
      await saveAppConfig(draft);
      savedMessage = "設定を保存しました";
      setTimeout(() => {
        savedMessage = "";
        if (!inline) onClose();
      }, 900);
    } finally {
      saving = false;
    }
  }
</script>

{#if draft}
  <div class="settings-panel {inline ? 'settings-inline' : 'settings-modal'}">
    <div class="settings-header">
      <h2>設定 — TWSNMP Earth</h2>
      {#if !inline}
        <button class="close-btn" onclick={onClose} aria-label="閉じる">✕</button>
      {/if}
    </div>

    <div class="settings-body">
      <section class="settings-section">
        <h3>監視拠点（自宅座標）</h3>
        <label>
          <span>拠点名</span>
          <input type="text" bind:value={draft.locationName} />
        </label>
        <div class="grid-2">
          <label>
            <span>緯度 (Latitude)</span>
            <input type="number" step="0.0001" bind:value={draft.latitude} />
          </label>
          <label>
            <span>経度 (Longitude)</span>
            <input type="number" step="0.0001" bind:value={draft.longitude} />
          </label>
        </div>
      </section>

      <section class="settings-section">
        <h3>アラート閾値</h3>
        <div class="grid-2">
          <label>
            <span>落雷接近半径 (km)</span>
            <input type="number" min="1" bind:value={draft.lightningRadiusKm} />
          </label>
          <label>
            <span>地震 通知最小震度</span>
            <input type="text" bind:value={draft.quakeMinIntensity} />
          </label>
          <label>
            <span>気圧急降下 (hPa/3h)</span>
            <input type="number" step="0.5" bind:value={draft.pressureDropHpa3h} />
          </label>
          <label>
            <span>磁気嵐 Kp指数閾値</span>
            <input type="number" step="0.5" bind:value={draft.kpIndexThreshold} />
          </label>
        </div>
      </section>

      <section class="settings-section">
        <h3>TWSNMP 外部通知連携</h3>
        <label class="checkbox-row">
          <input type="checkbox" bind:checked={draft.syslogEnabled} />
          <span>Syslog 通知を有効にする</span>
        </label>
        {#if draft.syslogEnabled}
          <div class="grid-2">
            <label>
              <span>Syslog ホスト</span>
              <input type="text" placeholder="192.168.1.10" bind:value={draft.syslogHost} />
            </label>
            <label>
              <span>ポート</span>
              <input type="number" bind:value={draft.syslogPort} />
            </label>
          </div>
        {/if}

        <label class="checkbox-row">
          <input type="checkbox" bind:checked={draft.mqttEnabled} />
          <span>MQTT 通知を有効にする</span>
        </label>
        {#if draft.mqttEnabled}
          <div class="grid-2">
            <label>
              <span>Broker URL</span>
              <input type="text" placeholder="tcp://localhost:1883" bind:value={draft.mqttBroker} />
            </label>
            <label>
              <span>Topic</span>
              <input type="text" bind:value={draft.mqttTopic} />
            </label>
          </div>
        {/if}

        <label class="checkbox-row">
          <input type="checkbox" bind:checked={draft.snmpTrapEnabled} />
          <span>SNMP Trap 通知を有効にする</span>
        </label>
        {#if draft.snmpTrapEnabled}
          <div class="grid-2">
            <label>
              <span>送信先 (Host:Port)</span>
              <input type="text" placeholder="192.168.1.10:162" bind:value={draft.snmpTrapTarget} />
            </label>
            <label>
              <span>Community</span>
              <input type="text" bind:value={draft.snmpCommunity} />
            </label>
          </div>
        {/if}

        {#if appState.platform.isMobile}
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={draft.notifyOnMobile} />
            <span>モバイル版でもフォアグラウンド時に外部通知を送信</span>
          </label>
        {/if}
      </section>

      <section class="settings-section">
        <h3>描画・プラットフォーム ({appState.platform.os}/{appState.platform.arch})</h3>
        <label class="checkbox-row">
          <input type="checkbox" bind:checked={draft.mobileLightMode} />
          <span>モバイル軽量描画モード（パーティクル数を抑制）</span>
        </label>
      </section>

      {#if appState.platform.canSelfUpdate}
        <section class="settings-section">
          <h3>アプリケーション更新（デスクトップ版）</h3>
          <label class="checkbox-row">
            <input type="checkbox" bind:checked={draft.autoCheckUpdate} />
            <span>起動時にアップデートを自動確認</span>
          </label>
          <div class="updater-box">
            <div>
              <div class="updater-meta">
                現在: v{appState.updater.currentVersion || "0.1.0"} / 状態: {appState.updater.state}
                {#if appState.updater.latestVersion}
                  （最新: v{appState.updater.latestVersion}）
                {/if}
              </div>
              {#if appState.updater.state === "downloading"}
                <div class="updater-progress">ダウンロード中... {appState.downloadProgress}%</div>
              {/if}
              {#if appState.updater.error}
                <div class="updater-error">{appState.updater.error}</div>
              {/if}
            </div>
            <div class="updater-actions">
              <button type="button" class="btn-secondary" onclick={checkForUpdate}>更新を確認</button>
              {#if appState.updater.state === "available"}
                <button type="button" class="btn-primary" onclick={installUpdate}>インストール</button>
              {:else if appState.updater.state === "ready"}
                <button type="button" class="btn-primary" onclick={restartApp}>再起動して適用</button>
              {/if}
            </div>
          </div>
        </section>
      {/if}
    </div>

    <div class="settings-footer">
      {#if savedMessage}
        <span class="saved-msg">{savedMessage}</span>
      {/if}
      <button type="button" class="btn-primary" disabled={saving} onclick={handleSave}>
        {saving ? "保存中..." : "設定を保存"}
      </button>
    </div>
  </div>
{/if}

<style>
  .settings-panel {
    background: #0d1222;
    border: 1px solid rgba(56, 189, 248, 0.25);
    border-radius: 12px;
    color: #e2e8f0;
    display: flex;
    flex-direction: column;
    max-height: 85vh;
    overflow: hidden;
  }
  .settings-inline {
    max-height: none;
    height: 100%;
    border: none;
    border-radius: 0;
  }
  .settings-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 18px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.15);
  }
  .settings-header h2 {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
    color: #38bdf8;
  }
  .close-btn {
    background: transparent;
    border: none;
    color: #94a3b8;
    font-size: 1.1rem;
    cursor: pointer;
  }
  .settings-body {
    padding: 16px 18px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .settings-section h3 {
    margin: 0 0 10px;
    font-size: 0.85rem;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 0.85rem;
    margin-bottom: 8px;
  }
  .checkbox-row {
    flex-direction: row;
    align-items: center;
    gap: 8px;
  }
  input[type="text"],
  input[type="number"] {
    background: #060914;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    padding: 7px 10px;
    color: #f8fafc;
    font-size: 0.9rem;
  }
  .grid-2 {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 10px;
  }
  .updater-box {
    background: #060914;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 8px;
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .updater-meta {
    font-size: 0.82rem;
    color: #cbd5e1;
  }
  .updater-progress {
    font-size: 0.8rem;
    color: #38bdf8;
  }
  .updater-error {
    font-size: 0.8rem;
    color: #f87171;
  }
  .updater-actions {
    display: flex;
    gap: 8px;
  }
  .settings-footer {
    padding: 12px 18px;
    border-top: 1px solid rgba(148, 163, 184, 0.15);
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: 12px;
  }
  .saved-msg {
    font-size: 0.85rem;
    color: #4ade80;
  }
  .btn-primary,
  .btn-secondary {
    border-radius: 6px;
    padding: 7px 14px;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    border: none;
  }
  .btn-primary {
    background: #0284c7;
    color: #fff;
  }
  .btn-secondary {
    background: #1e293b;
    color: #e2e8f0;
  }
</style>

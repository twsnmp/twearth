<script lang="ts">
  import { marked } from "marked";
  import DOMPurify from "dompurify";
  import * as AppService from "../../../bindings/github.com/twsnmp/twearth/internal/app/appservice";
  import type { ChatMessage } from "../../../bindings/github.com/twsnmp/twearth/internal/ai/models";
  import { appState, closeAIModal } from "../stores/appState.svelte";
  import { i18nState, locDisplayName, t } from "../i18n/index.svelte";

  type ActiveTab = "location" | "view" | "qa";

  let activeTab = $state<ActiveTab>("location");
  let targetLocationId = $state("");
  let loading = $state(false);
  let explanation = $state("");
  let error = $state("");
  let copied = $state(false);

  let chatMessages = $state<ChatMessage[]>([]);
  let chatInput = $state("");
  let chatLoading = $state(false);
  let chatError = $state("");
  let lastHandledSeq = 0;

  const isConfigured = $derived(
    Boolean(appState.config?.llmProvider && appState.config.llmProvider !== "none")
  );
  const locations = $derived(appState.config?.locations ?? []);
  const renderedExplanation = $derived(
    explanation ? DOMPurify.sanitize(marked.parse(explanation) as string) : ""
  );

  function renderMd(md: string): string {
    return DOMPurify.sanitize(marked.parse(md || "") as string);
  }

  function openAISettings() {
    closeAIModal();
    appState.settingsTab = "ai";
    appState.settingsOpen = true;
  }

  async function runLocationExplain() {
    if (!isConfigured) return;
    loading = true;
    error = "";
    explanation = "";
    try {
      const locId = targetLocationId || appState.selectedLocationId || locations[0]?.id || "";
      const resp = await AppService.LLMExplainLocation(locId, i18nState.lang);
      if (resp.error) {
        error = resp.error;
      } else {
        explanation = resp.content;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function runViewExplain() {
    if (!isConfigured) return;
    loading = true;
    error = "";
    explanation = "";
    try {
      if (appState.aiModal.mode === "event" && appState.aiModal.eventRecord) {
        const resp = await AppService.LLMExplainEvent(appState.aiModal.eventRecord, i18nState.lang);
        if (resp.error) error = resp.error;
        else explanation = resp.content;
      } else if (appState.aiModal.viewReq) {
        const req = { ...appState.aiModal.viewReq, lang: i18nState.lang };
        const resp = await AppService.LLMExplainView(req);
        if (resp.error) error = resp.error;
        else explanation = resp.content;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function sendQA(overrideText?: string) {
    const text = (overrideText ?? chatInput).trim();
    if (!text || chatLoading || !isConfigured) return;
    if (!overrideText) chatInput = "";
    chatError = "";
    const nextMessages: ChatMessage[] = [...chatMessages, { role: "user", content: text }];
    chatMessages = nextMessages;
    chatLoading = true;
    try {
      const resp = await AppService.LLMAsk({
        messages: nextMessages,
        locationId: targetLocationId || appState.selectedLocationId,
        extraContext: explanation ? explanation.slice(0, 2000) : "",
        lang: i18nState.lang,
      });
      if (resp.error) {
        chatError = resp.error;
      } else {
        chatMessages = [...nextMessages, { role: "assistant", content: resp.content }];
      }
    } catch (e) {
      chatError = e instanceof Error ? e.message : String(e);
    } finally {
      chatLoading = false;
    }
  }

  async function copyExplanation() {
    if (!explanation) return;
    await navigator.clipboard.writeText(explanation);
    copied = true;
    setTimeout(() => (copied = false), 2000);
  }

  $effect(() => {
    if (!appState.aiModal.open) return;
    if (appState.aiModal.seq === lastHandledSeq) return;
    lastHandledSeq = appState.aiModal.seq;
    targetLocationId = appState.aiModal.locationId || appState.selectedLocationId || locations[0]?.id || "";
    explanation = "";
    error = "";
    if (appState.aiModal.mode === "location") {
      activeTab = "location";
    } else if (appState.aiModal.mode === "view" || appState.aiModal.mode === "event") {
      activeTab = "view";
    } else {
      activeTab = "qa";
    }
  });
</script>

<div class="ai-card">
  <div class="ai-header">
    <div class="title-wrap">
      <span class="spark">✨</span>
      <h3>{t("ai.assistantTitle")}</h3>
      {#if isConfigured && appState.config?.llmModel}
        <span class="model-badge">{appState.config.llmProvider}: {appState.config.llmModel}</span>
      {/if}
    </div>
    <button class="close-btn" type="button" onclick={() => closeAIModal()} aria-label={t("app.close")}>✕</button>
  </div>

  <div class="tab-bar">
    <button
      type="button"
      class:active={activeTab === "location"}
      onclick={() => {
        if (activeTab !== "location") {
          activeTab = "location";
          explanation = "";
          error = "";
        }
      }}
    >
      {t("ai.tabLocation")}
    </button>
    {#if appState.aiModal.viewReq || appState.aiModal.eventRecord}
      <button
        type="button"
        class:active={activeTab === "view"}
        onclick={() => {
          if (activeTab !== "view") {
            activeTab = "view";
            explanation = "";
            error = "";
          }
        }}
      >
        {t("ai.tabView")}
      </button>
    {/if}
    <button type="button" class:active={activeTab === "qa"} onclick={() => (activeTab = "qa")}>
      {t("ai.tabQA")}
    </button>
  </div>

  <div class="ai-body">
    {#if !isConfigured}
      <div class="unconfigured-box">
        <h4>{t("ai.noProviderTitle")}</h4>
        <p>{t("ai.noProviderDesc")}</p>
        <button class="btn-primary" type="button" onclick={openAISettings}>{t("ai.openAISettings")}</button>
      </div>
    {:else if activeTab === "location" || activeTab === "view"}
      {#if activeTab === "location" && locations.length > 0}
        <div class="toolbar">
          <label class="loc-picker">
            <span>{t("ai.targetLocation")}:</span>
            <select
              bind:value={targetLocationId}
              onchange={() => {
                explanation = "";
                error = "";
              }}
            >
              {#each locations as loc (loc.id)}
                <option value={loc.id}>{locDisplayName(loc)}</option>
              {/each}
            </select>
          </label>
          <div class="tool-actions">
            {#if explanation && !loading}
              <button class="btn-sm" type="button" onclick={copyExplanation}>
                {copied ? t("ai.copied") : t("ai.copy")}
              </button>
            {/if}
            <button class="btn-primary-sm" type="button" disabled={loading} onclick={() => void runLocationExplain()}>
              {t("ai.reanalyze")}
            </button>
          </div>
        </div>
      {:else if activeTab === "view"}
        <div class="toolbar">
          <span class="view-label">
            {appState.aiModal.eventRecord
              ? appState.aiModal.eventRecord.title
              : `${appState.aiModal.viewReq?.view ?? ""} ${appState.aiModal.viewReq?.mode ?? ""}`}
          </span>
          <div class="tool-actions">
            {#if explanation && !loading}
              <button class="btn-sm" type="button" onclick={copyExplanation}>
                {copied ? t("ai.copied") : t("ai.copy")}
              </button>
            {/if}
            <button class="btn-primary-sm" type="button" disabled={loading} onclick={() => void runViewExplain()}>
              {t("ai.reanalyze")}
            </button>
          </div>
        </div>
      {/if}

      {#if loading}
        <div class="loading-box">
          <div class="spinner"></div>
          <span>{t("ai.analyzing")}</span>
        </div>
      {:else if error}
        <div class="err-box">{error}</div>
      {:else if explanation}
        <div class="md-box">
          {@html renderedExplanation}
        </div>
      {:else}
        <div class="ready-box">{t("ai.readyToAnalyze")}</div>
      {/if}
    {:else}
      <div class="qa-wrap">
        <div class="qa-messages">
          {#if chatMessages.length === 0}
            <div class="qa-welcome">
              <p>{t("ai.qaWelcome")}</p>
              <div class="quick-chips">
                <button type="button" onclick={() => void sendQA(t("ai.quickPrompt1"))}>{t("ai.quickPrompt1")}</button>
                <button type="button" onclick={() => void sendQA(t("ai.quickPrompt2"))}>{t("ai.quickPrompt2")}</button>
                <button type="button" onclick={() => void sendQA(t("ai.quickPrompt3"))}>{t("ai.quickPrompt3")}</button>
              </div>
            </div>
          {:else}
            {#each chatMessages as msg, idx (idx)}
              <div class="msg" class:user={msg.role === "user"} class:assistant={msg.role !== "user"}>
                <div class="msg-role">{msg.role === "user" ? "You" : "AI"}</div>
                <div class="msg-body md-inline">
                  {#if msg.role === "user"}
                    {msg.content}
                  {:else}
                    {@html renderMd(msg.content)}
                  {/if}
                </div>
              </div>
            {/each}
          {/if}
          {#if chatLoading}
            <div class="loading-inline">
              <div class="spinner sm"></div>
              <span>{t("ai.analyzing")}</span>
            </div>
          {/if}
          {#if chatError}
            <div class="err-box">{chatError}</div>
          {/if}
        </div>

        <form
          class="qa-input-row"
          onsubmit={(e) => {
            e.preventDefault();
            void sendQA();
          }}
        >
          <input
            type="text"
            bind:value={chatInput}
            placeholder={t("ai.askPlaceholder")}
            disabled={chatLoading}
          />
          <button class="btn-primary-sm" type="submit" disabled={chatLoading || !chatInput.trim()}>
            {t("ai.send")}
          </button>
          {#if chatMessages.length > 0}
            <button class="btn-sm" type="button" onclick={() => (chatMessages = [])}>
              {t("ai.clearChat")}
            </button>
          {/if}
        </form>
      </div>
    {/if}
  </div>
</div>

<style>
  .ai-card {
    background: #0d1222;
    border: 1px solid rgba(56, 189, 248, 0.35);
    border-radius: 12px;
    display: flex;
    flex-direction: column;
    height: min(82vh, 720px);
    color: #e2e8f0;
    box-shadow: 0 20px 50px rgba(0, 0, 0, 0.65);
  }
  .ai-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.16);
  }
  .title-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .title-wrap h3 {
    margin: 0;
    font-size: 0.98rem;
  }
  .model-badge {
    font-size: 0.68rem;
    padding: 2px 7px;
    border-radius: 999px;
    background: rgba(56, 189, 248, 0.16);
    color: #7dd3fc;
  }
  .close-btn {
    background: transparent;
    border: none;
    color: #94a3b8;
    font-size: 1rem;
    cursor: pointer;
  }
  .tab-bar {
    display: flex;
    gap: 4px;
    padding: 8px 16px 0;
    border-bottom: 1px solid rgba(148, 163, 184, 0.14);
  }
  .tab-bar button {
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: #94a3b8;
    padding: 6px 12px;
    font-size: 0.8rem;
    cursor: pointer;
  }
  .tab-bar button.active {
    color: #38bdf8;
    border-bottom-color: #38bdf8;
    font-weight: 600;
  }
  .ai-body {
    flex: 1;
    min-height: 0;
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow: hidden;
  }
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .loc-picker {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.78rem;
    color: #94a3b8;
  }
  .loc-picker select {
    background: #0b1224;
    border: 1px solid rgba(148, 163, 184, 0.25);
    border-radius: 6px;
    color: #e2e8f0;
    padding: 4px 8px;
    font-size: 0.78rem;
  }
  .view-label {
    font-size: 0.8rem;
    color: #7dd3fc;
    font-weight: 600;
  }
  .tool-actions {
    display: flex;
    gap: 6px;
  }
  .unconfigured-box {
    margin: auto;
    max-width: 440px;
    text-align: center;
    padding: 20px;
    background: rgba(15, 23, 42, 0.75);
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 10px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    align-items: center;
  }
  .unconfigured-box h4 {
    margin: 0;
    color: #fbbf24;
  }
  .unconfigured-box p {
    margin: 0;
    font-size: 0.82rem;
    color: #94a3b8;
    line-height: 1.5;
  }
  .loading-box,
  .ready-box {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    color: #94a3b8;
    font-size: 0.84rem;
    text-align: center;
    padding: 24px;
  }
  .loading-inline {
    display: flex;
    align-items: center;
    gap: 8px;
    color: #94a3b8;
    font-size: 0.78rem;
    padding: 6px 4px;
  }
  .spinner {
    width: 26px;
    height: 26px;
    border: 3px solid rgba(56, 189, 248, 0.2);
    border-top-color: #38bdf8;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  .spinner.sm {
    width: 15px;
    height: 15px;
    border-width: 2px;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .err-box {
    background: rgba(239, 68, 68, 0.16);
    border: 1px solid rgba(239, 68, 68, 0.4);
    color: #fca5a5;
    padding: 10px 12px;
    border-radius: 8px;
    font-size: 0.8rem;
  }
  .md-box {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 12px 14px;
    background: rgba(15, 23, 42, 0.72);
    border: 1px solid rgba(148, 163, 184, 0.16);
    border-radius: 8px;
    font-size: 0.84rem;
    line-height: 1.65;
  }
  .md-box :global(h1),
  .md-box :global(h2),
  .md-box :global(h3),
  .md-inline :global(h3) {
    color: #7dd3fc;
    margin: 0.7em 0 0.35em;
  }
  .md-box :global(p),
  .md-inline :global(p) {
    margin: 0.45em 0;
  }
  .md-box :global(ul),
  .md-box :global(ol),
  .md-inline :global(ul),
  .md-inline :global(ol) {
    padding-left: 1.3em;
    margin: 0.4em 0;
  }
  .md-box :global(strong),
  .md-inline :global(strong) {
    color: #f8fafc;
  }
  .qa-wrap {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .qa-messages {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 8px;
    background: rgba(15, 23, 42, 0.55);
    border: 1px solid rgba(148, 163, 184, 0.14);
    border-radius: 8px;
  }
  .qa-welcome {
    margin: auto;
    text-align: center;
    color: #94a3b8;
    font-size: 0.82rem;
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 480px;
  }
  .quick-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    justify-content: center;
  }
  .quick-chips button {
    background: rgba(14, 116, 144, 0.22);
    border: 1px solid rgba(56, 189, 248, 0.35);
    color: #bae6fd;
    border-radius: 999px;
    padding: 5px 10px;
    font-size: 0.74rem;
    cursor: pointer;
  }
  .quick-chips button:hover {
    border-color: #38bdf8;
    color: #ffffff;
  }
  .msg {
    padding: 8px 11px;
    border-radius: 8px;
    font-size: 0.82rem;
    line-height: 1.55;
  }
  .msg.user {
    align-self: flex-end;
    max-width: 82%;
    background: rgba(14, 116, 144, 0.32);
    border: 1px solid rgba(56, 189, 248, 0.4);
  }
  .msg.assistant {
    align-self: stretch;
    background: rgba(15, 23, 42, 0.85);
    border: 1px solid rgba(148, 163, 184, 0.16);
  }
  .msg-role {
    font-size: 0.68rem;
    font-weight: 700;
    color: #38bdf8;
    margin-bottom: 3px;
  }
  .qa-input-row {
    display: flex;
    gap: 8px;
  }
  .qa-input-row input {
    flex: 1;
    background: #0b1224;
    border: 1px solid rgba(148, 163, 184, 0.28);
    border-radius: 8px;
    color: #e2e8f0;
    padding: 8px 10px;
    font-size: 0.82rem;
  }
  .btn-sm,
  .btn-primary-sm,
  .btn-primary {
    border-radius: 6px;
    padding: 5px 11px;
    font-size: 0.78rem;
    cursor: pointer;
    border: 1px solid rgba(148, 163, 184, 0.25);
    background: #131c31;
    color: #e2e8f0;
  }
  .btn-primary-sm,
  .btn-primary {
    background: rgba(14, 116, 144, 0.5);
    border-color: rgba(56, 189, 248, 0.65);
    color: #ffffff;
    font-weight: 600;
  }
  .btn-primary {
    padding: 8px 16px;
  }
  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>

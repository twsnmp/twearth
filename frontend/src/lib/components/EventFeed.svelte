<script lang="ts">
  import { appState, focusMapAt } from "../stores/appState.svelte";
  import { i18nState, t } from "../i18n/index.svelte";
  import { localizeHypocenter, localizeLegacyEventText, localizeScale } from "../i18n/quakeText";

  const keyMap: Record<string, string> = {
    "event.pressureDrop.title": "events.pressureDropTitle",
    "event.pressureDrop.detail": "events.pressureDropDetail",
    "event.lightning.title": "events.lightningTitle",
    "event.lightning.detail": "events.lightningDetail",
    "event.space.title": "events.spaceTitle",
    "event.space.detail": "events.spaceDetail",
    "event.quake.title": "events.quakeTitle",
    "event.quake.detail": "events.quakeDetail",
    "event.tsunami.title": "events.tsunamiTitle",
    "event.eew.title": "events.eewTitle",
  };

  function resolveEventText(key: string | undefined, fallback: string, params?: Record<string, string | undefined> | null): string {
    if (key && keyMap[key]) {
      const normalizedParams = params
        ? {
            ...params,
            hypocenter: params.hypocenter ? localizeHypocenter(params.hypocenter) : undefined,
            maxScale: params.maxScale ? localizeScale(params.maxScale) : undefined,
          }
        : undefined;
      return t(keyMap[key], normalizedParams);
    }
    return localizeLegacyEventText(fallback);
  }

  function formatTime(ts: string): string {
    if (!ts) return "";
    const d = new Date(ts);
    if (Number.isNaN(d.getTime())) return ts;
    return d.toLocaleString(i18nState.lang === "ja" ? "ja-JP" : "en-US", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  }
</script>

<div class="feed-wrap">
  <div class="feed-header">
    <h3>{t("events.title")}</h3>
    <span>{appState.events.length}</span>
  </div>

  {#if appState.events.length === 0 && appState.quakes.length === 0}
    <div class="empty">{t("events.empty")}</div>
  {:else}
    <ul class="feed-list">
      {#each appState.events as ev (ev.id + ev.timestamp)}
        <li>
          <button
            type="button"
            class="feed-item"
            onclick={() => {
              if (ev.latitude !== undefined && ev.longitude !== undefined) focusMapAt(ev.latitude, ev.longitude, 7);
            }}
          >
            <div class="item-top">
              <span class="type-badge {ev.severity}">{ev.type}</span>
              <span class="time">{formatTime(ev.timestamp)}</span>
            </div>
            <div class="title">{resolveEventText(ev.titleKey, ev.title, ev.params)}</div>
            {#if ev.detail || ev.detailKey}
              <div class="detail">{resolveEventText(ev.detailKey, ev.detail, ev.params)}</div>
            {/if}
          </button>
        </li>
      {/each}

      {#each appState.quakes.slice(0, 15) as q (q.id)}
        <li>
          <button
            type="button"
            class="feed-item"
            onclick={() => {
              if (q.latitude || q.longitude) focusMapAt(q.latitude, q.longitude, 6.5);
            }}
          >
            <div class="item-top">
              <span class="type-badge info">{q.source.toUpperCase()}</span>
              <span class="time">{formatTime(q.occurredAt)}</span>
            </div>
            <div class="title">{localizeLegacyEventText(q.title)}</div>
            {#if q.hypocenter}
              <div class="detail">{localizeHypocenter(q.hypocenter)} ({q.depthKm} km)</div>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .feed-wrap {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .feed-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 14px;
    border-bottom: 1px solid rgba(148, 163, 184, 0.14);
    font-size: 0.8rem;
    color: #94a3b8;
  }
  .feed-header h3 {
    margin: 0;
    font-size: 0.85rem;
    color: #e2e8f0;
  }
  .empty {
    margin: auto;
    color: #64748b;
    font-size: 0.85rem;
  }
  .feed-list {
    list-style: none;
    margin: 0;
    padding: 8px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .feed-item {
    width: 100%;
    text-align: left;
    background: #070b17;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 8px;
    padding: 8px 10px;
    color: #e2e8f0;
    cursor: pointer;
  }
  .feed-item:hover {
    border-color: rgba(56, 189, 248, 0.45);
  }
  .item-top {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 4px;
  }
  .type-badge {
    font-size: 0.68rem;
    padding: 1px 6px;
    border-radius: 4px;
    background: rgba(56, 189, 248, 0.15);
    color: #38bdf8;
  }
  .type-badge.warning,
  .type-badge.critical {
    background: rgba(248, 113, 113, 0.2);
    color: #fca5a5;
  }
  .time {
    font-size: 0.72rem;
    color: #64748b;
  }
  .title {
    font-size: 0.84rem;
    font-weight: 600;
  }
  .detail {
    font-size: 0.76rem;
    color: #94a3b8;
    margin-top: 2px;
  }
</style>

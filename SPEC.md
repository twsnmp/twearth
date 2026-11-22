# twearth (TWSNMP Earth) 仕様書 (SPEC.md)

## 1. プロジェクト概要
`twearth` は、TWSNMPシリーズの新しいクロスプラットフォーム（デスクトップ＆モバイル）アプリケーションである。
ユーザーが居住する地域および日本・地球規模の自然環境データ（気圧、偏西風の蛇行、海水温、地震、火山、落雷、宇宙天気など）を、**ユーザー登録・APIキー不要のオープンAPI**からリアルタイムに取得し、ビジュアルに表示・監視する。
また、異常値や特定地域の災害イベントを検知した際には、Syslog / SNMP Trap / MQTT / Webhook を通じて `TWSNMP FC` / `TWSNMP FK` / `TWSNMP NEO` などの外部監視システムへ通知を送る「地球環境センサー」としての役割を持つ（常時監視・通知はデスクトップ版を中心に運用し、モバイル版では主にビューワー・ローカルアラート・任意送信として動作する）。

### 1.1 ターゲットプラットフォーム
- **デスクトップ版:** macOS / Windows / Linux (Wails v3 ネイティブアプリ + 自己アップデート対応)
- **モバイル版:** iOS / Android (Wails v3 モバイルサポート機能を利用、単一コードベースからビルド)

---

## 2. 技術スタック・開発環境

### 2.1 ビルド・ランタイム管理
- **環境管理:** `mise` (`mise.toml` にて Go, Node.js, Wails CLI 等のバージョンを固定・タスク管理)
- **ビルドタスク:** Wails v3 の `Taskfile.yml` ワークフロー（デスクトップビルド、`wails3 task ios:*`、`wails3 task android:*`）
  - **iOS 開発要件:** macOS + Xcode (iOS SDK) + CGO
  * **Android 開発要件:** JDK + Android SDK / NDK + CGO

### 2.2 バックエンド (Go)
- **フレームワーク:** Wails v3 (`github.com/wailsapp/wails/v3`)
- **言語:** Go (1.27+)
- **組み込みKVS:** `go.etcd.io/bbolt` (設定、キャッシュ、時系列観測履歴データの保存。プラットフォーム別のアプリデータディレクトリに格納)
- **プラットフォーム分離:** Go ビルドタグ (`//go:build !android && !ios` / `//go:build android || ios`) によりデスクトップ限定機能（自己アップデート等）とモバイル固有設定を分離
- **自己アップデート（デスクトップ版のみ）:** Wails v3 `app.Updater` (`pkg/updater` + `providers/github`, リポジトリ: `twsnmp/twearth`)
- **通信・連携:**
  - HTTP/REST クライアント (標準 `net/http` + 適切な User-Agent 付与とレート制限管理)
  - WebSocket クライアント (P2P地震情報リアルタイム受信用。モバイル版はフォアグラウンド復帰時の再接続・履歴補完に対応)
  - 外部通知送信: Syslog (`log/syslog` 互換パケット送信), SNMP Trap (`gosnmp`), MQTT (`paho.mqtt.golang`)

### 2.3 フロントエンド (Svelte 5 + TypeScript)
- **フレームワーク:** Svelte 5 (Runes: `$state`, `$derived`, `$effect` を全面採用) + Vite
- **スタイリング:** Tailwind CSS (ダークモード基調のサイバー・監視ダッシュボードUI)
- **地図エンジン:** `MapLibre GL JS` (OpenStreetMap ダークタイル / 国土地理院タイル / 気象庁ラスタタイル)
- **パーティクル・物理表現:** `p5.js` (MapLibre のキャンバス上にオーバーレイし、偏西風の蛇行ベクトル場・落雷閃光・震源波紋・降灰拡散をアニメーション描画)
- **チャート・ゲージ:** `Apache ECharts` (気圧推移、CAPE、海水温、太陽フラックス、Kp指数などの時系列グラフとメーター)

---

## 3. データソース仕様（すべてユーザー登録・APIキー不要）

バックエンドの Go サービスが各 API を定期ポーリング（または WebSocket 常時接続）し、キャッシュおよび正規化してフロントエンドへ Wails Events 経由で配信する。

### 3.1 気象・偏西風・大気環境 (Open-Meteo API)
- **Weather Forecast API:** `https://api.open-meteo.com/v1/forecast`
  - **自宅・指定拠点データ (15分〜1時間周期):**
    - `surface_pressure`, `pressure_msl` (気圧・気圧変化量)
    - `temperature_2m`, `relative_humidity_2m`, `precipitation`, `weather_code`
    - `cape` (対流有効位置エネルギー：雷・大気不安定度の指標)
    - `shortwave_radiation`, `soil_temperature_0cm`, `soil_moisture_0_to_1cm`
  - **偏西風グリッドデータ (1〜3時間周期):**
    - 日本周辺（例：北緯20°〜50°、東経120°〜155°を2.5°〜5°刻み）の複数座標を一括リクエスト
    - `wind_speed_250hPa`, `wind_direction_250hPa` (上空約10kmのジェット気流・偏西風)
    - `wind_speed_500hPa`, `wind_direction_500hPa`, `geopotential_height_500hPa` (上空約5,500m・火山灰拡散や寒気流入の指標)
- **Marine Weather API:** `https://marine-api.open-meteo.com/v1/marine`
  - **海水温・海洋データ (1〜3時間周期):**
    - メイン観測都市を基準に自動選定される近海・広域代表海域およびエルニーニョ関連監視海域（`NINO.3`, `NINO.WEST`, `IOBW` 等）の `sea_surface_temperature` (海面水温: SST), `wave_height`, `ocean_current_velocity`, `ocean_current_direction`（`cell_selection=sea` 指定、手動追加・カスタム座標登録と併用可能）
- **Air Quality API:** `https://air-quality-api.open-meteo.com/v1/air-quality`
  - **大気質データ (1時間周期):**
    - `pm2_5`, `pm10`, `dust` (黄砂), `uv_index`

### 3.2 地震・津波 (P2P地震情報 API / 気象庁 / USGS / J-SHIS)
- **P2P地震情報 WebSocket API v2 (常時接続):**
  - `wss://api.p2pquake.net/v2/ws` (履歴補完は `https://api.p2pquake.net/v2/history?codes=551&limit=20`)
  - 震源・震度情報 (code: 551)、津波予報 (code: 552)、緊急地震速報 (code: 556) をリアルタイム受信
- **気象庁 地震リスト JSON (バックアップ・詳細補完):**
  - `https://www.jma.go.jp/bosai/quake/data/list.json`
- **USGS Earthquake API (世界規模の地震・15分周期):**
  - `https://earthquake.usgs.gov/earthquakes/feed/v1.0/summary/4.5_day.geojson`
- **防災科研 J-SHIS API (拠点登録時に取得・キャッシュ):**
  - 表層地盤増幅率・微地形区分を取得し、自宅座標の揺れやすさ基礎情報として表示

### 3.3 火山情報 (気象庁 JSON / XML)
- **活火山マスター一覧 (起動時取得):**
  - `https://www.jma.go.jp/bosai/volcano/const/volcano_list.json`
- **現在の噴火警戒レベル一覧 (15分周期):**
  - `https://www.jma.go.jp/bosai/volcano/data/warning.json`
- **防災情報XML (噴火速報・降灰予報・1〜5分周期):**
  - `https://www.data.jma.go.jp/developer/xml/feed/eqvol.xml`

### 3.4 雷・ナウキャスト・アメダス (気象庁 LIDEN / タイル)
- **LIDEN 雷観測データ (5分周期):**
  1. `https://www.jma.go.jp/bosai/jmatile/data/nowc/targetTimes_N3.json` から最新の `basetime`, `validtime` を取得
  2. `https://www.jma.go.jp/bosai/jmatile/data/nowc/{basetime}/none/{validtime}/surf/liden/data.geojson?id=liden` から落雷・雲放電の座標（GeoJSON）を取得
  3. 自宅座標からの距離をヒュベニの公式（またはHaversine）で計算し、指定半径（例：20km）以内の落雷数をカウント

### 3.5 宇宙天気・地球観測 (NOAA SWPC / JAXA)
- **NOAA Space Weather Prediction Center (15分周期):**
  - 地磁気嵐 Kp指数: `https://services.swpc.noaa.gov/json/planetary_k_index_1m.json`
  - 太陽フレア X線フラックス: `https://services.swpc.noaa.gov/json/goes/primary/xrays-6-hour.json`
- **ベースマップ・オーバーレイタイル:**
  - 国土地理院タイル（淡色地図・陰影起伏図・ハザードマップ）
  - 気象庁 海面水温・雨雲・雷ナウキャスト ラスタタイル（必要に応じて MapLibre レイヤーに合成）

---

## 4. アーキテクチャ・ディレクトリ構成

```text
twearth/
├── mise.toml                 # Go, Node, Wails v3 のバージョン・ビルドタスク定義
├── Taskfile.yml              # Wails v3 デスクトップ / iOS / Android ビルドタスク
├── build/                    # Wails v3 プラットフォーム別ビルド設定・アセット (darwin, windows, linux, ios, android)
├── main.go                   # Wails v3 アプリケーションエントリポイント
├── go.mod
├── internal/
│   ├── app/                  # Wails バインディング・ライフサイクル管理
│   ├── platform/             # OS別データパス解決・プラットフォーム判定 (ビルドタグ分離)
│   ├── updater/              # 自己アップデート制御 (//go:build !android && !ios デスクトップ限定)
│   ├── store/                # bbolt データベース操作 (Config, TimeSeries, EventLog)
│   ├── collector/            # 各オープンAPIのフェッチャー・スケジューラ
│   │   ├── openmeteo.go      # 気圧・偏西風グリッド・海水温・CAPE取得
│   │   ├── quake.go          # P2P地震情報 WebSocket & USGS GeoJSON
│   │   ├── volcano.go        # 気象庁 火山一覧・警戒レベル・XML
│   │   ├── liden.go          # 気象庁 LIDEN 落雷GeoJSON・接近判定
│   │   └── space.go          # NOAA SWPC 宇宙天気取得
│   └── notifier/             # 閾値判定 & Syslog / SNMP Trap / MQTT 送信
└── frontend/                 # Svelte 5 + Vite + Tailwind CSS (レスポンシブ: デスクトップ & モバイル)
    ├── src/
    │   ├── App.svelte        # メインレイアウト（デスクトップ: 地図+パネル / モバイル: タブ・セーフエリア対応）
    │   ├── lib/
    │   │   ├── components/
    │   │   │   ├── MapCanvas.svelte     # MapLibre GL JS + p5.js オーバーレイ統合
    │   │   │   ├── JetStreamP5.ts       # p5.js による偏西風パーティクル・波紋描画ロジック
    │   │   │   ├── ChartsPanel.svelte   # ECharts による気圧・水温・CAPE・宇宙天気チャート
    │   │   │   ├── EventFeed.svelte     # 地震・落雷・火山・アラート履歴リスト
    │   │   │   └── SettingsModal.svelte # 自宅座標・ポーリング間隔・通知設定・(デスクトップのみ)アップデート確認
    │   │   └── stores/                  # Svelte 5 Runes ($state) によるリアクティブ状態管理
    └── package.json
```

---

## 5. コア機能の実装詳細

### 5.1 MapLibre GL JS + p5.js の同期オーバーレイ
1. `MapLibre GL JS` で日本〜東アジア（または地球全体）のダークテーマ地図を描画する。
2. 地図コンテナの上に透過の `p5.js` キャンバス（`pointer-events: none`）を絶対配置で重ねる。
3. MapLibre の `move`, `zoom`, `resize` イベントにフックし、`map.project([lng, lat])` を使って地理座標とスクリーンピクセル座標をリアルタイム変換する。
4. **偏西風（250hPa）の可視化:**
   - グリッドデータから双線形補間（Bilinear Interpolation）で任意座標の風向・風速ベクトル $(u, v)$ を算出。
   - 数千個のパーティクルを p5.js 上で移動させ、風速に応じて色（青→緑→黄→赤→紫）と軌跡の長さ（残像）を変化させることで、偏西風の蛇行を直感的に表現する。
5. **イベントエフェクト:**
   - **地震:** 震源座標を中心に、マグニチュードと最大震度に応じた色・半径の同心円波紋アニメーションを描画。
   - **落雷 (LIDEN):** 直近5分間の落雷座標に閃光パーティクルを描画し、時間経過でフェードアウトさせる。
   - **火山:** 活火山アイコンを噴火警戒レベル別に色分けし、噴火・レベル上昇時は500hPaの風向ベクトルに沿って降灰予測方向のパーティクルを流す。

### 5.2 Apache ECharts ダッシュボードパネル
- **ローカル環境モニター（自宅座標）:**
  - 海面更生気圧（hPa）の過去72時間〜1週間の推移グラフ（急激な気圧低下時にアラートライン表示）。
  - 大気不安定度（CAPE）と自宅半径20km以内の5分間落雷カウントの複合グラフ。
- **海洋・宇宙モニター:**
  - 最寄り海域の海面水温（SST）推移と平年差イメージ。
  - NOAA 太陽X線フラックスと Kp指数ゲージ。

### 5.3 `bbolt` データスキーマ
- `config` バケット:
  - 自宅の緯度・経度・名称（デフォルト値：埼玉県富士見市付近 `lat: 35.8569, lon: 139.5492` 等、任意変更可能）
  - アラート閾値（落雷接近距離 km、地震震度閾値、気圧低下量 hPa/3h、Kp指数閾値）
  - 通知先設定（Syslog サーバー IP/Port、MQTT Broker URL/Topic、SNMP Trap 送信先）
- `timeseries` バケット:
  - キー: `YYYYMMDDHHmm` (UTCまたはJSTタイムスタンプ)
  - 値: JSON または MessagePack 形式のローカル観測スナップショット（気圧、気温、CAPE、近海SST、落雷数、Kp指数）
- `events` バケット:
  - 検知した地震、落雷接近、火山警戒レベル変化、気圧急降下イベントの履歴

### 5.4 TWSNMP 連携（Syslog / MQTT / Trap）
- 以下のイベント発生時に RFC3164/RFC5424 形式の Syslog または JSON ペイロードの MQTT メッセージを送信する：
  - `TWEARTH-QUAKE`: 自宅周辺または全国で指定震度以上の地震を検知
  - `TWEARTH-LIGHTNING`: 自宅から半径 $N$ km 以内に落雷を検知（電源・通信機器保護アラート）
  - `TWEARTH-PRESSURE`: 3時間で $X$ hPa 以上の急激な気圧低下を検知
  - `TWEARTH-VOLCANO`: 活火山の噴火速報または警戒レベル上昇を検知
  - `TWEARTH-SPACE`: 太陽フレア（M/Xクラス）または磁気嵐（Kp 5以上）を検知

### 5.5 モバイル対応・レスポンシブ設計 (iOS / Android)
- **レスポンシブ UI:**
  - デスクトップ（大画面）では地図を背景にサイド・ボトムパネルを同時表示し、モバイル（縦画面・狭画面）ではボトムナビゲーションによる「地図 / チャート / イベント / 設定」のタブ切替レイアウトを提供する。
  - モバイル端末のノッチ・ホームインジケータを考慮し、`viewport-fit=cover` および `env(safe-area-inset-*)` に対応する。
- **描画負荷の動的制御:**
  - モバイル環境ではバッテリー消費とフレームレート維持のため、`p5.js` の偏西風パーティクル数および描画レートを自動的に軽量化する（設定で変更可能）。
- **バックグラウンド・ネットワーク復帰:**
  - モバイル OS によるバックグラウンド通信制限に対応するため、フォアグラウンド復帰時に P2P地震情報 WebSocket の再接続および API データの即時リフレッシュ（履歴補完）を実行する。

### 5.6 自己アップデート機能（デスクトップ版のみ）
- **対象:** デスクトップ版（macOS / Windows / Linux）のみ。モバイル版はビルドタグ（`!android && !ios`）により組み込まず、UI にもアップデート関連項目を表示しない。
- **実装方式:** Wails v3 標準の `app.Updater`（`github.com/wailsapp/wails/v3/pkg/updater`）および `providers/github`（リポジトリ: `twsnmp/twearth`）を使用する。
- **更新フロー:**
  - 起動時の自動チェック（設定で ON/OFF 可）および設定画面からの手動チェックを提供。
  - `updater:*` イベントをフロントエンドで購読し、更新検知・ダウンロード進捗・SHA-256/署名検証・インストール・再起動案内を UI 上に表示する。

---

## 6. 開発フェーズ（AIエージェント向け実装ステップ）

- **Phase 1: プロジェクト初期化と基盤構築**
  - `mise.toml` / `Taskfile.yml` の作成、Wails v3（デスクトップ + モバイル構成）+ Svelte 5 + Tailwind CSS の初期化。
  - `internal/platform/`（ビルドタグによるパス解決・プラットフォーム判定）および `internal/updater/`（デスクトップ限定自己アップデート）の基盤実装。
  - `bbolt` ストアの実装と、レスポンシブ基本レイアウト・設定画面（自宅座標・通知先設定・アップデートUI）の構築。
- **Phase 2: バックエンド Collector の実装**
  - Open-Meteo (気象・250hPa偏西風グリッド・Marine SST)、気象庁 (火山・LIDEN落雷)、P2P地震情報 (WebSocket)、NOAA SWPC の各クライアントを実装し、Wails Events でフロントへ流す。
  - モバイル版フォアグラウンド復帰時の再接続・差分キャッチアップ制御を実装。
- **Phase 3: MapLibre + p5.js ビジュアライザの実装**
  - ベースマップ表示、偏西風パーティクルアニメーション（モバイル軽量モード対応）、海水温ヒートレイヤー、地震・火山・落雷マーカーとエフェクトの実装。
- **Phase 4: ECharts チャートパネルと TWSNMP 通知連携・プラットフォーム検証**
  - 時系列データの `bbolt` 保存と ECharts グラフ描画。
  - Syslog / MQTT / SNMP Trap 送信機能の結合とテスト。
  - デスクトップ版ビルド＆自己アップデート動作確認、iOS/Android モバイルビルドの疎通確認。

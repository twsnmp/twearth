import { i18nState, t } from "./index.svelte";

const PLACE_TOKENS: [string, string][] = [
  ["北海道", "Hokkaido"],
  ["青森県", "Aomori"],
  ["岩手県", "Iwate"],
  ["宮城県", "Miyagi"],
  ["秋田県", "Akita"],
  ["山形県", "Yamagata"],
  ["福島県", "Fukushima"],
  ["茨城県", "Ibaraki"],
  ["栃木県", "Tochigi"],
  ["群馬県", "Gunma"],
  ["埼玉県", "Saitama"],
  ["千葉県", "Chiba"],
  ["東京都", "Tokyo"],
  ["神奈川県", "Kanagawa"],
  ["新潟県", "Niigata"],
  ["富山県", "Toyama"],
  ["石川県", "Ishikawa"],
  ["福井県", "Fukui"],
  ["山梨県", "Yamanashi"],
  ["長野県", "Nagano"],
  ["岐阜県", "Gifu"],
  ["静岡県", "Shizuoka"],
  ["愛知県", "Aichi"],
  ["三重県", "Mie"],
  ["滋賀県", "Shiga"],
  ["京都府", "Kyoto"],
  ["大阪府", "Osaka"],
  ["兵庫県", "Hyogo"],
  ["奈良県", "Nara"],
  ["和歌山県", "Wakayama"],
  ["鳥取県", "Tottori"],
  ["島根県", "Shimane"],
  ["岡山県", "Okayama"],
  ["広島県", "Hiroshima"],
  ["山口県", "Yamaguchi"],
  ["徳島県", "Tokushima"],
  ["香川県", "Kagawa"],
  ["愛媛県", "Ehime"],
  ["高知県", "Kochi"],
  ["福岡県", "Fukuoka"],
  ["佐賀県", "Saga"],
  ["長崎県", "Nagasaki"],
  ["熊本県", "Kumamoto"],
  ["大分県", "Oita"],
  ["宮崎県", "Miyazaki"],
  ["鹿児島県", "Kagoshima"],
  ["沖縄本島", "Okinawa Main Island"],
  ["沖縄", "Okinawa"],
  ["トカラ列島", "Tokara Islands"],
  ["小笠原諸島", "Ogasawara Islands"],
  ["伊豆諸島", "Izu Islands"],
  ["千島列島", "Kuril Islands"],
  ["能登半島", "Noto Peninsula"],
  ["紀伊水道", "Kii Channel"],
  ["豊後水道", "Bungo Channel"],
  ["日向灘", "Hyuga-nada Sea"],
  ["相模湾", "Sagami Bay"],
  ["東京湾", "Tokyo Bay"],
  ["駿河湾", "Suruga Bay"],
  ["日本海", "Sea of Japan"],
  ["東シナ海", "East China Sea"],
  ["天草・芦北", "Amakusa-Ashikita"],
  ["薩摩", "Satsuma"],
  ["大隅", "Osumi"],
  ["十勝", "Tokachi"],
  ["根室", "Nemuro"],
  ["釧路", "Kushiro"],
  ["浦河", "Urakawa"],
  ["胆振", "Iburi"],
  ["三陸", "Sanriku"],
  ["房総", "Boso"],
  ["伊豆", "Izu"],
  ["東海道", "Tokaido"],
  ["南海", "Nankai"],
  ["北方", "Northern"],
  ["南方", "Southern"],
  ["東方", "Eastern"],
  ["西方", "Western"],
  ["北西部", "NW"],
  ["北東部", "NE"],
  ["南西部", "SW"],
  ["南東部", "SE"],
  ["北部", "North"],
  ["南部", "South"],
  ["東部", "East"],
  ["西部", "West"],
  ["中部", "Central"],
  ["中越", "Chuetsu"],
  ["上越", "Joetsu"],
  ["下越", "Kaetsu"],
  ["近海", " Near Sea"],
  ["半島", " Peninsula"],
  ["諸島", " Islands"],
  ["列島", " Islands"],
  ["地方", " Region"],
  ["沖", " Offshore"],
  ["湾", " Bay"],
  ["海峡", " Strait"],
  ["周辺", " Area"],
];

export function localizeScale(scale: string): string {
  if (i18nState.lang !== "en") return scale;
  return scale
    .replace("5弱", "5-")
    .replace("5強", "5+")
    .replace("6弱", "6-")
    .replace("6強", "6+");
}

export function localizeHypocenter(raw: string): string {
  if (!raw || i18nState.lang !== "en") return raw;
  let out = raw;
  for (const [jaToken, enToken] of PLACE_TOKENS) {
    if (out.includes(jaToken)) {
      out = out.split(jaToken).join(` ${enToken} `);
    }
  }
  return out.replace(/\s+/g, " ").trim();
}

export function localizeLegacyEventText(raw: string): string {
  if (!raw || i18nState.lang !== "en") return raw;

  if (raw === "津波予報発表") return t("events.tsunamiTitle");
  if (raw === "緊急地震速報 (EEW)") return t("events.eewTitle");

  // Match "地震 <hypocenter> M<mag> 最大震度<scale>"
  const qMatch = raw.match(/^地震\s+(.+?)\s+M([0-9.]+)\s+最大震度(.+)$/);
  if (qMatch) {
    return t("events.quakeTitle", {
      hypocenter: localizeHypocenter(qMatch[1]),
      magnitude: qMatch[2],
      maxScale: localizeScale(qMatch[3]),
    });
  }

  // Match "宇宙天気アラート Kp=<kp> / フレア=<flare>"
  const spMatch = raw.match(/^宇宙天気アラート\s+Kp=([0-9.]+)\s+\/\s+フレア=(.+)$/);
  if (spMatch) {
    return t("events.spaceTitle", { kp: spMatch[1], flare: spMatch[2] });
  }

  // Match "<loc> <radius>km 圏内で落雷検知 (<count>件)"
  const lidMatch = raw.match(/^(.+?)\s+([0-9.]+)km\s+圏内で落雷検知\s+\((\d+)件\)$/);
  if (lidMatch) {
    return t("events.lightningTitle", {
      location: lidMatch[1],
      radius: lidMatch[2],
      count: lidMatch[3],
    });
  }

  const obsMatch = raw.match(/^観測時刻:\s*(.+)$/);
  if (obsMatch) {
    return t("events.lightningDetail", { time: obsMatch[1] });
  }

  return localizeHypocenter(raw);
}

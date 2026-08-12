/**
 * 播放地址规则识别。
 *
 * 目的：让 Player 根据 URL 后缀 / host 自动决定走哪条播放链路：
 *   - native  → 直连 MP4/WebM（浏览器原生 <video>）；
 *   - hls     → .m3u8，优先用 hls.js（Safari 原生支持 / 桌面走 MSE）；
 *   - dash    → .mpd，dash.js（MSE）；
 *   - youtube → youtu.be / youtube.com，自动嵌 iframe；
 *   - bilibili→ bilibili.com，自动嵌 iframe；
 *   - unknown → 兜底 iframe embed，URL 直接放进 src（演示 / 内部占位）。
 *
 * 注意：本模块不依赖 hls.js / dash.js（包未引入）；当规则命中 hls / dash 时，
 * Player 在浏览器原生不支持 MSE 的情况下退化为外链 iframe，确保不卡死。
 */

export type PlaybackKind = "native" | "hls" | "dash" | "youtube" | "bilibili" | "iframe";

interface Detection {
    kind: PlaybackKind;
    /** 若是 iframe / youtube / bilibili，给出最终嵌入 URL。 */
    embedUrl: string;
    /** 原始地址，仅做调试用。 */
    sourceUrl: string;
}

function getHost(url: string): string {
    try {
        return new URL(url).host.toLowerCase();
    } catch {
        return "";
    }
}

function getPath(url: string): string {
    try {
        return new URL(url).pathname.toLowerCase();
    } catch {
        return "";
    }
}

function toYouTubeEmbed(url: string): string {
    try {
        const u = new URL(url);
        if (u.hostname === "youtu.be") {
            const id = u.pathname.replace(/^\//, "");
            return `https://www.youtube.com/embed/${id}`;
        }
        if (u.hostname.endsWith("youtube.com")) {
            const id = u.searchParams.get("v");
            if (id) return `https://www.youtube.com/embed/${id}`;
            if (u.pathname.startsWith("/embed/")) return url;
            if (u.pathname.startsWith("/shorts/")) {
                return `https://www.youtube.com/embed/${u.pathname.replace("/shorts/", "")}`;
            }
        }
    } catch {
        /* fall through */
    }
    return url;
}

function toBilibiliEmbed(url: string): string {
    try {
        const u = new URL(url);
        const m = u.pathname.match(/\/video\/(BV[a-zA-Z0-9]+|av\d+)/);
        if (m) {
            const p = u.searchParams.get("p") ?? "1";
            return `https://player.bilibili.com/player.html?bvid=${m[1]}&autoplay=0&p=${p}`;
        }
    } catch {
        /* fall through */
    }
    return url;
}

/**
 * 解析播放地址的种类与最终嵌入 URL。
 * 仅根据字符串特征判定，不发起网络请求。
 */
export function detectPlayback(url: string | null | undefined): Detection | null {
    if (!url) return null;
    const host = getHost(url);
    const path = getPath(url);

    if (host === "youtu.be" || host.endsWith("youtube.com") || host.endsWith("youtube-nocookie.com")) {
        return {kind: "youtube", embedUrl: toYouTubeEmbed(url), sourceUrl: url};
    }
    if (host.endsWith("bilibili.com") || host.endsWith("b23.tv")) {
        return {kind: "bilibili", embedUrl: toBilibiliEmbed(url), sourceUrl: url};
    }

    if (path.endsWith(".m3u8")) {
        return {kind: "hls", embedUrl: url, sourceUrl: url};
    }
    if (path.endsWith(".mpd")) {
        return {kind: "dash", embedUrl: url, sourceUrl: url};
    }
    if (/\.(mp4|webm|ogv|m4v|mov)(\?|$)/.test(path)) {
        return {kind: "native", embedUrl: url, sourceUrl: url};
    }
    return {kind: "iframe", embedUrl: url, sourceUrl: url};
}

/** 给 UI 展示用：把规则翻译成一行中文。 */
export function describePlayback(detection: Detection | null): string {
    if (!detection) return "尚未选择播放源";
    switch (detection.kind) {
        case "native": return "MP4 直链（原生播放器）";
        case "hls": return "HLS 流（自适应码率）";
        case "dash": return "DASH 流（自适应码率）";
        case "youtube": return "YouTube 外链";
        case "bilibili": return "哔哩哔哩外链";
        case "iframe": return "通用 iframe 嵌入";
    }
}
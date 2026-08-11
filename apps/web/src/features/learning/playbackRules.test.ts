/**
 * playbackRules 单元测试 —— 覆盖 URL 识别与中文描述。
 */

import {describe, expect, it} from "vitest";

import {detectPlayback, describePlayback} from "./playbackRules";

describe("detectPlayback", () => {
    it("returns null for empty / nullish input", () => {
        expect(detectPlayback("")).toBeNull();
        expect(detectPlayback(null)).toBeNull();
        expect(detectPlayback(undefined)).toBeNull();
    });

    it("detects MP4 / WebM / MOV as native", () => {
        expect(detectPlayback("https://cdn.example.com/lesson.mp4")?.kind).toBe("native");
        expect(detectPlayback("https://cdn.example.com/clip.webm")?.kind).toBe("native");
        expect(detectPlayback("https://cdn.example.com/intro.mov")?.kind).toBe("native");
        expect(detectPlayback("https://cdn.example.com/clip.mp4?token=abc")?.kind).toBe("native");
    });

    it("detects HLS (.m3u8)", () => {
        expect(detectPlayback("https://cdn.example.com/master.m3u8")?.kind).toBe("hls");
    });

    it("detects DASH (.mpd)", () => {
        expect(detectPlayback("https://cdn.example.com/manifest.mpd")?.kind).toBe("dash");
    });

    it("detects YouTube (watch / shorts / youtu.be)", () => {
        expect(detectPlayback("https://www.youtube.com/watch?v=dQw4w9WgXcQ")?.kind).toBe("youtube");
        expect(detectPlayback("https://youtu.be/dQw4w9WgXcQ")?.kind).toBe("youtube");
        expect(detectPlayback("https://www.youtube.com/shorts/abc123")?.kind).toBe("youtube");
    });

    it("rewrites YouTube watch URL to /embed/ form", () => {
        const det = detectPlayback("https://www.youtube.com/watch?v=dQw4w9WgXcQ");
        expect(det?.embedUrl).toBe("https://www.youtube.com/embed/dQw4w9WgXcQ");
    });

    it("rewrites youtu.be to embed URL", () => {
        const det = detectPlayback("https://youtu.be/abcDEF12345");
        expect(det?.embedUrl).toBe("https://www.youtube.com/embed/abcDEF12345");
    });

    it("detects Bilibili / b23.tv and rewrites to player.html", () => {
        const det = detectPlayback("https://www.bilibili.com/video/BV1xx411c7mD?p=2");
        expect(det?.kind).toBe("bilibili");
        expect(det?.embedUrl).toContain("player.bilibili.com/player.html");
        expect(det?.embedUrl).toContain("bvid=BV1xx411c7mD");
        expect(det?.embedUrl).toContain("p=2");

        const short = detectPlayback("https://b23.tv/abcXYZ");
        expect(short?.kind).toBe("bilibili");
    });

    it("falls back to iframe for unknown URLs", () => {
        const det = detectPlayback("https://internal.example.com/player/abc");
        expect(det?.kind).toBe("iframe");
        expect(det?.embedUrl).toBe("https://internal.example.com/player/abc");
    });
});

describe("describePlayback", () => {
    it("returns a Chinese label for each kind", () => {
        expect(describePlayback(detectPlayback("https://x.com/a.mp4"))).toMatch(/原生/);
        expect(describePlayback(detectPlayback("https://x.com/a.m3u8"))).toMatch(/HLS/);
        expect(describePlayback(detectPlayback("https://x.com/a.mpd"))).toMatch(/DASH/);
        expect(describePlayback(detectPlayback("https://youtu.be/abc"))).toMatch(/YouTube/);
        expect(describePlayback(detectPlayback("https://www.bilibili.com/video/BV1xx"))).toMatch(/哔哩哔哩/);
        expect(describePlayback(detectPlayback("https://example.com/x"))).toMatch(/iframe/);
    });

    it("returns a placeholder for null detection", () => {
        expect(describePlayback(null)).toMatch(/尚未选择/);
    });
});
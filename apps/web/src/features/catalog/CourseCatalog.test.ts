import {describe, expect, it} from "vitest";
import {buildCourseQuery, type Course} from "../../api/types";
import {formatCourseDuration, formatCoursePrice, formatCourseStats} from "./CourseCatalog";

const baseCourse: Course = {
    id: "c1", teacherId: "t1", slug: "evm-deep-dive", title: "EVM 深入", description: "",
    status: "published", currentVersion: 3, priceMinor: 12900, currency: "USD",
    createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z",
};

describe("course catalog helpers", () => {
    it("formats free and paid prices", () => {
        expect(formatCoursePrice({priceMinor: 0, currency: "USD"})).toBe("免费");
        expect(formatCoursePrice({priceMinor: 1299, currency: "USD"})).toMatch(/12\.99/);
    });

    it("builds a stable encoded query", () => {
        expect(buildCourseQuery({q: " web3 security ", priceMax: 5000, limit: 9})).toBe("q=web3+security&priceMax=5000&limit=9");
    });

    it("formats durations below and above an hour", () => {
        expect(formatCourseDuration(0)).toBe("");
        expect(formatCourseDuration(45 * 60)).toBe("45 分钟");
        expect(formatCourseDuration(60 * 60)).toBe("1 小时");
        expect(formatCourseDuration(90 * 60)).toBe("1.5 小时");
    });

    it("joins available stats and omits missing aggregates", () => {
        expect(formatCourseStats({...baseCourse, chapterCount: 5, lessonCount: 22, durationSeconds: 7200}))
            .toBe("5 章 · 22 课时 · 2 小时");
        expect(formatCourseStats(baseCourse)).toBe("");
    });
});

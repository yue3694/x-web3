import {describe, expect, it} from "vitest";

import {normalizeCourseKey} from "./derive";

describe("normalizeCourseKey", () => {
    const key = "a".repeat(64);

    it("adds the viem bytes32 prefix to API course keys", () => {
        expect(normalizeCourseKey(key)).toBe(`0x${key}`);
    });

    it("keeps and normalizes prefixed course keys", () => {
        expect(normalizeCourseKey(`0x${key.toUpperCase()}`)).toBe(`0x${key}`);
    });

    it("rejects malformed values", () => {
        expect(() => normalizeCourseKey("0x1234")).toThrow("Invalid courseKey");
    });
});

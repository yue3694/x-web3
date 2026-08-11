import {describe, expect, it} from "vitest";
import {buildCourseQuery} from "../../api/types";
import {formatCoursePrice} from "./CourseCatalog";

describe("course catalog helpers", () => {
    it("formats free and paid prices", () => {
        expect(formatCoursePrice({priceMinor: 0, currency: "USD"})).toBe("免费");
        expect(formatCoursePrice({priceMinor: 1299, currency: "USD"})).toMatch(/12\.99/);
    });

    it("builds a stable encoded query", () => {
        expect(buildCourseQuery({q: " web3 security ", priceMax: 5000, limit: 9})).toBe("q=web3+security&priceMax=5000&limit=9");
    });
});

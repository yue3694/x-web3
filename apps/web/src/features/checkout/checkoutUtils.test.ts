import {describe, expect, it} from "vitest";

import {buttonLabel} from "./checkoutUtils";

describe("buttonLabel", () => {
    it("formats YD wei using 18 token decimals", () => {
        expect(buttonLabel({
            isConnected: true,
            onWrongChain: false,
            isSwitching: false,
            state: "idle",
            priceYD: "149000000000000000000",
            receiptLoading: false,
        })).toBe("用 149 YD 购买");
    });
});

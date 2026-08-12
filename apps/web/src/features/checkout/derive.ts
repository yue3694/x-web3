/**
 * F03 链上 / 链下标识对齐 helpers。
 *
 * 1. courseKeyFromUuid：前端用 sha256(uuid) 计算课程键（与后端
 *    `apps/api/internal/order/order.go::CourseKey` 对齐）。合约事件
 *    CoursePurchased(courseKey, ...) 的 courseKey 必须与此一致。
 *    算法 SSOT：api / web / worker test fixture 三端都用 SHA-256。
 *    历史上 doc 曾误写为 keccak256；实际是 SHA-256 + 32 字节 hex。
 *    合约侧把 courseKey 当 mapping key 不验证内容；改算法需同步
 *    api/web/worker 三处 + ABI stub 注释。
 *
 * 2. uuidToBytes16：把 intent.id(UUID 字符串) 切成高 128 位 big-endian
 *    bytes16（合约事件里的 intentId 字段；worker 用其匹配
 *    purchase_intents.id 的高 128 位）。低 128 位丢弃是因为合约事件
 *    字段是 bytes16。
 *
 * 两个 helper 都没有运行时依赖；浏览器自带的 Web Crypto + TextEncoder 即可。
 */

const HEX_32 = /^[0-9a-fA-F]{32}$/;
const HEX_64 = /^[0-9a-fA-F]{64}$/;

/** API 使用无 0x 的 64 位 hex，viem 使用 0x-prefixed bytes32；统一为后者。 */
export function normalizeCourseKey(value: string): `0x${string}` {
    const hex = value.startsWith("0x") ? value.slice(2) : value;
    if (!HEX_64.test(hex)) {
        throw new Error("Invalid courseKey returned by the purchase intent");
    }
    return (`0x${hex.toLowerCase()}`) as `0x${string}`;
}

/**
 * 计算课程的链上 key：sha256(uuid_bytes) → 32 字节 hex（带 0x）。
 *
 * @param courseId UUID 字符串（含连字符，如 "f0e8..."）
 * @returns 0x 开头的 64 字符 hex
 */
export async function courseKeyFromUuid(courseId: string): Promise<`0x${string}`> {
    const canonical = courseId.replace(/-/g, "").toLowerCase();
    if (!HEX_32.test(canonical)) {
        throw new Error(`courseKeyFromUuid: invalid uuid ${courseId}`);
    }
    const bytes = new Uint8Array(16);
    for (let i = 0; i < 16; i++) {
        bytes[i] = parseInt(canonical.slice(i * 2, i * 2 + 2), 16);
    }
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    return ("0x" + bytesToHex(new Uint8Array(digest))) as `0x${string}`;
}

/**
 * 把 UUID 字符串切成高 128 位 big-endian 的 0x-prefixed 16 字节 hex。
 *
 * 与合约 CoursePurchased(intentId) 的 bytes16 字段对齐；worker 在链事件解码
 * 时也只取高 128 位，因此后端 purchase_intents.id 的高 128 位必须能落到这 16 字节上。
 *
 * @param id UUID 字符串（含连字符或裸 32 字符 hex）
 */
export function uuidToBytes16(id: string): `0x${string}` {
    const canonical = id.replace(/-/g, "").toLowerCase();
    if (!HEX_32.test(canonical)) {
        throw new Error(`uuidToBytes16: invalid uuid ${id}`);
    }
    // UUID v4 是 16 字节；高 128 位 = 前 16 hex 字符 = canonical 前 8 hex chars? No —
    // 16 bytes = 32 hex chars；canonical 就是 32 hex chars，所以高 128 位就是全部。
    // 这里整体保留（与后端 worker 的 high128 一致）。
    return ("0x" + canonical) as `0x${string}`;
}

function bytesToHex(bytes: Uint8Array): string {
    let out = "";
    for (let i = 0; i < bytes.length; i++) {
        out += bytes[i].toString(16).padStart(2, "0");
    }
    return out;
}

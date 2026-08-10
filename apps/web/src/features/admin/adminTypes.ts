/**
 * 共享 Admin TS 类型。
 *
 * 字段命名与后端 handler 强对齐：
 *   - User 列表 / 角色授予 → `apps/api/internal/admin/handlers/users.go`
 *   - Chain sync           → `apps/api/internal/admin/handlers/chain_sync.go`
 *     （与 packages/shared/openapi/chain-sync.yaml 中
 *      `nextBlock` / `lagSeconds` / `lastUpdatedAt` 字段命名一致）
 *   - DLQ                  → `apps/api/internal/admin/handlers/dlq_store.go`
 *     （DlqRow 字段以 openapi/chain-sync.yaml 的 DlqRow 为准）
 *   - Audit                → `apps/api/internal/audit`（actor / action / at）
 *
 * 注意：admin/openapi 契约尚未在本仓落盘（packages/shared/openapi/admin.yaml
 * 仍由 spec-writer 编写中），下列字段在契约落地后必须再核对一次以避免 drift。
 */

// ---------- Users / Roles ----------

/** 角色 code（与 auth.Permission 表对齐）。 */
export type AdminRoleCode = "student" | "teacher" | "super_admin";

/** 单条用户记录（admin 视图）。 */
export interface AdminUser {
    id: string;
    email: string;
    displayName: string;
    walletsCount: number;
    roles: AdminRoleCode[];
    createdAt: string;
    lastLoginAt: string | null;
}

/** 单条角色记录（授予/撤销弹窗用）。 */
export interface AdminRole {
    code: AdminRoleCode;
    label: string;
    description: string;
}

/** 角色授予/撤销请求体。 */
export interface AdminRoleChangeRequest {
    role: AdminRoleCode;
    reason?: string;
}

/** 用户列表分页响应。 */
export interface AdminUserPage {
    items: AdminUser[];
    page: number;
    pageSize: number;
    total: number;
}

// ---------- Chain sync ----------

/** 单链同步状态（对齐 chain-sync.yaml DlqRow 上游 chain 状态字段）。 */
export interface ChainSyncStatus {
    chainId: number;
    nextBlock: number;
    lagSeconds: number;
    lastUpdatedAt: string;
    /** 同步器健康态。 */
    healthy: boolean;
}

// ---------- DLQ ----------

/** DLQ 单条（对齐 openapi/chain-sync.yaml DlqRow）。 */
export interface DlqEntry {
    id: number;
    consumer: string;
    chainId: number | null;
    kind: string;
    severity: string;
    summary: string;
    payload: Record<string, unknown>;
    retryCount: number;
    resolved: boolean;
    createdAt: string;
    resolution: "replayed" | "ignored" | "manual" | null;
}

/** DLQ 列表响应。 */
export interface DlqListResponse {
    items: DlqEntry[];
    count: number;
}

/** DLQ 重试请求体（resolution ∈ replayed / ignored / manual）。 */
export interface DlqRetryRequest {
    resolution: "replayed" | "ignored" | "manual";
}

// ---------- Audit ----------

/** 审计条目。 */
export interface AuditEntry {
    id: string;
    actorUserId: string;
    actorDisplayName?: string;
    action: string;
    target: string;
    at: string;
    payload: Record<string, unknown>;
}

/** 审计查询参数。 */
export interface AuditQuery {
    actorUserId?: string;
    action?: string;
    from?: string;
    to?: string;
}

/** 审计列表响应。 */
export interface AuditListResponse {
    items: AuditEntry[];
}

// ---------- Chain rewind（Cert Retry 等场景复用） ----------

/** Rewind 请求体（与 chain-sync.yaml RewindRequest 对齐）。 */
export interface ChainRewindRequest {
    chainId: number;
    fromBlock: number;
    reason?: string;
}

/**
 * Admin 视图层 API 包装：
 *   - 全部走 @/api/client 的 `apiClient`（统一注入 X-Request-ID / credentials）
 *   - 类型从 ./adminTypes 引入，与 packages/shared/openapi 对齐
 *
 * 鉴权：所有 endpoint 都在后端 main.go 装载 `auth + rbac.Middleware(PermSystemAdmin)`，
 * handler 内部仍做二次校验（defense in depth）。前端 hasPermission 仅用作 UX 隐藏。
 *
 * 路径命名说明：
 *   - 与 packages/shared/openapi/chain-sync.yaml 一致的 DlqRow / Rewind* 沿用同一路径
 *   - Users / Roles / Audit / Certificates Retry 当前契约尚未在 openapi 落盘，
 *     暂以 RESTful 约定命名，admin.yaml 落地后必须再校准。
 */

import {apiClient} from "@/api/client";

import type {
    AdminRoleChangeRequest,
    AdminCourseReviewQueue,
    AdminCourseReviewItem,
    AdminUserPage,
    AuditListResponse,
    AuditQuery,
    ChainRewindRequest,
    ChainSyncStatus,
    DlqListResponse,
    DlqRetryRequest,
    DlqRetryResponse,
} from "./adminTypes";

function buildQuery(params: Record<string, string | number | undefined>): string {
    const sp = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
        if (v === undefined || v === null || v === "") continue;
        sp.set(k, String(v));
    }
    const s = sp.toString();
    return s ? `?${s}` : "";
}

export const adminApi = {
    // ---------- Course review ----------

    async listCoursesForReview(): Promise<AdminCourseReviewQueue> {
        return apiClient.get<AdminCourseReviewQueue>("/admin/courses");
    },

    async reviewCourse(courseId: string, action: "approve" | "reject", reason = ""): Promise<AdminCourseReviewItem> {
        return apiClient.post<AdminCourseReviewItem>(`/admin/courses/${courseId}/review`, {action, reason});
    },

    // ---------- Users / Roles ----------

    /**
     * GET /admin/users?page=&pageSize=
     * 后端分页：page 从 1 起，pageSize 默认 20。
     */
    async listUsers(page = 1, pageSize = 20): Promise<AdminUserPage> {
        return apiClient.get<AdminUserPage>(
            `/admin/users${buildQuery({page, pageSize})}`,
        );
    },

    /**
     * POST /admin/users/:id/roles  (grant)
     * body: { role, reason? }
     */
    async grantRole(userId: string, req: AdminRoleChangeRequest): Promise<void> {
        await apiClient.post<void>(`/admin/users/${userId}/roles`, req);
    },

    /**
     * DELETE /admin/users/:id/roles/:role  (revoke)
     * reason 通过 query string 透传给后端留 audit。
     */
    async revokeRole(userId: string, role: string, reason?: string): Promise<void> {
        const qs = buildQuery({reason});
        await apiClient.delete<void>(`/admin/users/${userId}/roles/${role}${qs}`);
    },

    // ---------- Chain sync ----------

    /**
     * GET /admin/chain/sync?chainId=
     * 返回单链状态：nextBlock / lagSeconds / lastUpdatedAt。
     */
    async getChainSync(chainId: number): Promise<ChainSyncStatus> {
        return apiClient.get<ChainSyncStatus>(
            `/admin/chain/sync${buildQuery({chainId})}`,
        );
    },

    /**
     * POST /admin/chain/rewind
     * 与 packages/shared/openapi/chain-sync.yaml 的 rewindChain 对齐。
     */
    async rewindChain(req: ChainRewindRequest): Promise<void> {
        await apiClient.post<void>("/admin/chain/rewind", req);
    },

    // ---------- DLQ ----------

    /**
     * GET /admin/dlq?limit=
     * 列表 unresolved DLQ；上限 500。
     */
    async listDlq(limit = 100): Promise<DlqListResponse> {
        return apiClient.get<DlqListResponse>(`/admin/dlq${buildQuery({limit})}`);
    },

    /**
     * POST /admin/dlq/:id/retry
     * body: { resolution: replayed | ignored | manual }
     */
    async retryDlq(id: number, req: DlqRetryRequest): Promise<DlqRetryResponse> {
        return apiClient.post<DlqRetryResponse>(`/admin/dlq/${id}/retry`, req);
    },

    // ---------- Audit ----------

    /**
     * GET /admin/audit?actorUserId=&action=&from=&to=
     * 全部查询参数可选；返回按 at 倒序。
     */
    async searchAudit(query: AuditQuery): Promise<AuditListResponse> {
        return apiClient.get<AuditListResponse>(`/admin/audit${buildQuery(query)}`);
    },

    // ---------- Certificates Retry ----------

    /**
     * POST /admin/certificates/:id/retry
     * 触发证书重发（worker 重新入队 certificate_jobs）。
     */
    async retryCertificate(certificateId: string, reason?: string): Promise<void> {
        await apiClient.post<void>(`/admin/certificates/${certificateId}/retry`, {
            reason,
        });
    },
};

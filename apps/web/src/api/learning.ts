/**
 * 学习进度 / 课程完成 / 我的报名 API。
 *
 * 字段对齐 `packages/shared/openapi/learning.yaml`（live spec，F04）：
 *   - reportLessonProgress → POST /lessons/{id}/progress
 *     请求：{ pct: 0..100 整数 }；响应：{ lessonId, pct }。
 *   - listMyEnrollments    → GET  /me/enrollments?limit=
 *   - markCourseComplete   → POST /courses/{id}/complete
 *     返回 CompletionRecord（包含证书 recipientWallet / onchainCertId / status 等）。
 *
 * 注意：F04 重构后证书不再走 `/me/certificates`，而是来自
 * `/courses/{id}/complete` 的 CompletionRecord；本模块暂不暴露
 * 证书列表（前端 MyCertificates 单独由 CompleteCourse 触发或后续接入）。
 *
 * 设计要点：
 *   - 进度上报失败静默：上游 Player / ProgressReporter 已做节流与单调推进；
 *     这里仅在 404/405/501（后端未实现）时吞掉，其它错误向上抛。
 *   - 报名列表使用 limit 分页（无 cursor，与最新 spec 对齐）。
 */

import {apiClient, ApiClientError} from "./client";

/** 我的报名单条记录（对齐 EnrollmentItem）。 */
export interface EnrollmentItem {
    enrollmentId: string;
    courseId: string;
    courseSlug: string;
    courseTitle: string;
    enrolledAt: string;
    requiredLessonsTotal: number;
    completedLessonsTotal: number;
    completionPct: number;
    hasCompletion: boolean;
    completedAt?: string | null;
}

/** 我的报名列表响应（对齐 EnrollmentListResponse）。 */
export interface EnrollmentListResponse {
    items: EnrollmentItem[];
}

/** 课程完课记录（对齐 CompletionRecord；包含证书状态与链上信息）。 */
export interface CompletionRecord {
    id: string;
    enrollmentId: string;
    userId: string;
    courseId: string;
    ruleVersion: number;
    completedAt: string;
    completedLessonsCount: number;
    totalLessonsCount: number;
    certificateId?: string | null;
    onchainCertId: string;
    /** certificate_jobs 状态机：pending / minting / confirmed / failed / dead */
    status: "pending" | "minting" | "confirmed" | "failed" | "dead";
    recipientWallet: `0x${string}`;
    metadataUri: string;
    metadataSha256: string;
}

/** 课时进度上报请求（对齐 ProgressReportRequest）。 */
export interface ProgressReportRequest {
    pct: number;
}

/** 课时进度上报响应（对齐 ProgressReportResponse）。 */
export interface ProgressReportResponse {
    lessonId: string;
    pct: number;
}

/** 后端"未挂载 / 暂未实现" 的状态码：进度上报在此被吞掉。 */
const SOFT_MISSING = new Set([404, 405, 501]);

export const learningApi = {
    /**
     * 进度上报：失败静默（不打断视频）。
     * - 404/405/501 → 后端尚未实现 → 吞掉
     * - 其它错误 → 吞掉（与 Player 上报策略保持一致）
     */
    async reportLessonProgress(lessonId: string, req: ProgressReportRequest): Promise<void> {
        try {
            await apiClient.post<ProgressReportResponse>(`/lessons/${lessonId}/progress`, req);
        } catch (e: unknown) {
            if (e instanceof ApiClientError && SOFT_MISSING.has(e.status)) return;
        }
    },

    /**
     * 标记课程完成：服务端二次校验 required lessons 全部 pct=100，
     * 否则返回 422 (`BAD_REQUEST` / partial completion)。
     * 成功返回 CompletionRecord；幂等。
     */
    async markCourseComplete(courseId: string): Promise<CompletionRecord> {
        return apiClient.post<CompletionRecord>(`/courses/${courseId}/complete`);
    },

    /** 我的报名列表（limit 分页；默认 50、上限 50）。 */
    async listMyEnrollments(limit = 50): Promise<EnrollmentListResponse> {
        return apiClient.get<EnrollmentListResponse>(`/me/enrollments?limit=${limit}`);
    },
};

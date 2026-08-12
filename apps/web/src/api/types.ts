/**
 * F01 切片的 API 客户端：
 *   - loginWithPrivy → POST /auth/privy/session
 *   - logout → DELETE /auth/session
 *   - getMe → GET /me
 *   - linkWallet → POST /me/wallets/link
 *   - unbindWallet → DELETE /me/wallets/{id}
 *
 * 这些函数与 packages/shared/openapi/auth.yaml 一一对应。
 */

import {apiClient} from "./client";

export type RoleCode = "student" | "teacher" | "super_admin";

export interface Wallet {
    id: string;
    chainId: number;
    address: string;
    isPrimary: boolean;
    boundAt: string;
}

export interface Profile {
    id: string;
    displayName: string;
    primaryWallet: Wallet | null;
    wallets: Wallet[];
    roles: RoleCode[];
    permissions: string[];
}

export interface WalletLinkRequest {
    chainId: number;
    address: string;
    nonce: string;
    expiry: string;
    signature: string;
    domain?: string;
}

export interface WalletNonce {
	nonce: string;
	domain: string;
	expiresAt: string;
}

export interface WalletLoginChallenge extends WalletNonce {
    registered: boolean;
    displayName: string;
}

export const authApi = {
    async loginWithPrivy(privyAccessToken: string): Promise<Profile> {
        return apiClient.post<Profile>("/auth/privy/session", {
            privyAccessToken,
        });
    },
    async logout(): Promise<void> {
        await apiClient.delete<void>("/auth/session");
    },
    async getMe(): Promise<Profile | null> {
        try {
            return await apiClient.get<Profile>("/me");
        } catch (e: unknown) {
            // 401 = not logged in → return null instead of throwing
            if (
                e instanceof Error &&
                "status" in e &&
                (e as {status: number}).status === 401
            ) {
                return null;
            }
            throw e;
        }
    },
    async issueWalletLoginNonce(chainId: number, address: string): Promise<WalletLoginChallenge> {
        return apiClient.post<WalletLoginChallenge>("/auth/wallet/nonce", {chainId, address});
    },
    async loginWithWallet(req: WalletLinkRequest & {displayName?: string}): Promise<Profile> {
        return apiClient.post<Profile>("/auth/wallet/session", req);
    },
    async updateProfile(displayName: string): Promise<Profile> {
        return apiClient.patch<Profile>("/me", {displayName});
    },
	async linkWallet(req: WalletLinkRequest): Promise<{wallets: Wallet[]}> {
        return apiClient.post<{wallets: Wallet[]}>("/me/wallets/link", req);
	},
	async issueWalletNonce(): Promise<WalletNonce> {
		return apiClient.post<WalletNonce>("/me/wallets/nonce");
	},
    async unbindWallet(walletId: string): Promise<void> {
        await apiClient.delete<void>(`/me/wallets/${walletId}`);
    },
};

/**
 * F02 切片的播放凭证：
 *   - issuePlayback → GET /lessons/{id}/playback
 *     返回 S3 presigned GET（或 CloudFront signed cookie），
 *     TTL ≤ 5 分钟，过期需重新签发。
 *   - reportProgress → POST /lessons/{id}/progress
 *     T13 阶段后端 F04 尚未完成，这里走"软占位"：先发到 /lessons/{id}/progress，
 *     收到 404/405 时静默退化为本地缓存，避免打断播放。
 *     真实幂等写与最大进度单调推进留待 F04。
 */
export interface PlaybackCredential {
    lessonId: string;
    url: string;
    expiresAt: string;
    purpose: "playback" | "preview";
}

export interface ProgressReport {
    /** 当前播放位置（秒，浮点） */
    positionSeconds: number;
    /** 总时长（秒，浮点；不可知时传 0） */
    durationSeconds: number;
    /** 进度万分位 0..10000（与 F04 design.md 对齐） */
    progressBps: number;
    /** 客户端记录时间（ISO 8601），用于服务端做时钟漂移校正 */
    reportedAt: string;
}

export const learningApi = {
    async issuePlayback(lessonId: string): Promise<PlaybackCredential> {
        return apiClient.get<PlaybackCredential>(`/lessons/${lessonId}/playback`);
    },
    /**
     * 进度上报占位：当前阶段后端未实现，按 F04 设计的路径发送。
     * 失败统一吞掉，不影响视频播放。
     */
    async reportProgress(lessonId: string, report: ProgressReport): Promise<void> {
        try {
            await apiClient.post<void>(`/lessons/${lessonId}/progress`, report);
        } catch (e: unknown) {
            if (e instanceof Error && "status" in e) {
                const status = (e as {status: number}).status;
                // 404/405 = 后端尚未实现（F04 阶段），静默忽略。
                if (status === 404 || status === 405 || status === 501) return;
            }
            // 其它错误（网络/5xx）也吞掉，避免打断播放体验。
        }
    },
};

export type CourseStatus = "draft" | "pending_review" | "published" | "archived";

export interface Course {
    id: string;
    teacherId: string;
    teacherName?: string;
    slug: string;
    title: string;
    description: string;
    status: CourseStatus;
    currentVersion: number;
    priceMinor: number;
    currency: string;
    /** 当前版本的章节数；旧版后端可能不返回，消费方需容错。 */
    chapterCount?: number;
    /** 当前版本的课时数。 */
    lessonCount?: number;
    /** 当前版本所有课时时长之和（秒）。 */
    durationSeconds?: number;
    publishedAt?: string;
    createdAt: string;
    updatedAt: string;
}

export interface CoursePage {
    items: Course[];
    nextCursor: string;
}

export interface CourseDetail {
    course: Course;
    chapters: CourseChapter[];
    enrolled: boolean;
}

export interface CourseChapter {
    id: string;
    position: number;
    title: string;
    lessons: Array<{id: string; position: number; title: string; required: boolean; durationSeconds: number; mediaAssetId?: string}>;
}

export interface CourseWriteRequest {
    slug?: string;
    title: string;
    description: string;
    priceMinor: number;
    currency: string;
}

/**
 * Curriculum write payload（F02-T12）。
 *
 * 顺序由数组下标决定；后端 ReplaceCurriculum 会落 position。
 * mediaAssetId 可选——已 finalize 的 media_assets.id；未上传则 null。
 */
export interface CurriculumLessonInput {
    title: string;
    required: boolean;
    durationSeconds: number;
    mediaAssetId?: string | null;
}

export interface CurriculumChapterInput {
    title: string;
    lessons: CurriculumLessonInput[];
}

export interface CurriculumWriteRequest {
    chapters: CurriculumChapterInput[];
}

/**
 * 后端 PUT /teacher/courses/:id/curriculum 返回的 ETag 包装。
 * 返回 {currentVersion, chapters}；chapters 字段保留服务端 position 排序。
 */
export interface CurriculumWriteResponse {
    currentVersion: number;
    chapters: Array<{
        title: string;
        lessons: Array<{
            title: string;
            required: boolean;
            durationSeconds: number;
            mediaAssetId?: string | null;
        }>;
    }>;
}

export function buildCourseQuery(input: {q?: string; priceMax?: number; before?: string; limit?: number}): string {
    const query = new URLSearchParams();
    if (input.q?.trim()) query.set("q", input.q.trim());
    if (input.priceMax !== undefined) query.set("priceMax", String(input.priceMax));
    if (input.before) query.set("before", input.before);
    query.set("limit", String(input.limit ?? 9));
    return query.toString();
}

export const courseApi = {
    listMine(): Promise<{items: Array<{course: Course; chapters: CourseChapter[]}>}> {
        return apiClient.get<{items: Array<{course: Course; chapters: CourseChapter[]}>}>("/teacher/courses");
    },
    list(input: {q?: string; priceMax?: number; before?: string; limit?: number} = {}): Promise<CoursePage> {
        return apiClient.get<CoursePage>(`/courses?${buildCourseQuery(input)}`);
    },
    get(id: string): Promise<CourseDetail> {
        return apiClient.get<CourseDetail>(`/courses/${id}`);
    },
    create(input: CourseWriteRequest & {slug: string}): Promise<Course> {
        return apiClient.post<Course>("/teacher/courses", input);
    },
    update(id: string, version: number, input: CourseWriteRequest): Promise<Course> {
        return apiClient.put<Course>(`/teacher/courses/${id}`, input, {headers: {"If-Match": String(version)}});
    },
    submit(id: string): Promise<Course> {
        return apiClient.post<Course>(`/teacher/courses/${id}/submit`);
    },
    /**
     * 替换课程章节/课时（PUT 整体替换；后端走乐观锁 If-Match）。
     * 失败时 ApiClientError.code === 'STALE_VERSION' → UI 提示刷新。
     */
    replaceCurriculum(id: string, version: number, body: CurriculumWriteRequest): Promise<CurriculumWriteResponse> {
        return apiClient.put<CurriculumWriteResponse>(`/teacher/courses/${id}/curriculum`, body, {headers: {"If-Match": String(version)}});
    },
};

/**
 * 评论 (F02-T09) — 对应 packages/shared/openapi/course.yaml 中 /courses/{id}/comments
 * 与 /courses/comments/{id}。
 *
 * 业务规则（见 apps/api/internal/comment/comment.go）：
 *   - 只有已购买用户可写评论；后端在 COMMENT_NOT_PURCHASED 时返回 403。
 *   - moderation_status ∈ pending | approved | rejected；默认 pending。
 *   - 自己写的评论无论状态都返回给别人看，自己全部状态可见。
 *   - 软删除 deleted_at 非空 → 列表查询过滤；用户自己 DELETE 走 /courses/comments/{id}。
 */
export type ModerationStatus = "pending" | "approved" | "rejected";

export interface Comment {
    id: string;
    courseId: string;
    userId: string;
    userDisplayName?: string;
    body: string;
    moderationStatus: ModerationStatus;
    createdAt: string;
    updatedAt: string;
}

export interface CommentPage {
    items: Comment[];
}

export const commentApi = {
    /** 课程评论列表（未登录也能调；后端自然只返回 approved）。 */
    listByCourse(courseId: string, limit = 50): Promise<CommentPage> {
        return apiClient.get<CommentPage>(`/courses/${courseId}/comments?limit=${limit}`);
    },
    /** 已购买用户在课程下写评论；moderation 由后端默认成 pending。 */
    create(courseId: string, body: string): Promise<Comment> {
        return apiClient.post<Comment>(`/courses/${courseId}/comments`, {body});
    },
    /** 软删除自己的评论；后端返回 204。 */
    softDelete(commentId: string): Promise<void> {
        return apiClient.delete<void>(`/courses/comments/${commentId}`);
    },
    /**
     * 我的评论列表（含 pending / rejected / approved）。
     * 对应后端 comment.Repo.ListMyByUser。
     *
     * 注意：当前后端 main.go 尚未挂载此路由（与 packages/shared/openapi/course.yaml
     * 一致地约定为 GET /me/comments）；调用方需在缺失时优雅降级。
     */
    listMine(limit = 50): Promise<CommentPage> {
        return apiClient.get<CommentPage>(`/me/comments?limit=${limit}`);
    },
};

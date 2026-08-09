/**
 * 通用 fetch 封装：
 *   - 默认带 credentials: 'include'（让 sid cookie 自动发送）
 *   - 解析后端 ApiError envelope，抛出 ApiClientError
 *   - 不重试 4xx；5xx 由调用方决定
 *
 * 禁止在组件里直接 fetch；所有 HTTP 调用都走这里以便统一注入：
 *   - X-Request-ID（便于服务端日志关联）
 *   - correlationId
 *   - error mapping
 */

import {type ApiError as ApiErrorType, ErrorCode} from "@x-web3/shared";

export class ApiClientError extends Error {
    readonly code: string;
    readonly status: number;
    readonly requestId: string;
    readonly details: Record<string, unknown> | undefined;

    constructor(env: ApiErrorType, status: number) {
        super(env.message);
        this.name = "ApiClientError";
        this.code = env.code;
        this.status = status;
        this.requestId = env.requestId;
        this.details = env.details;
    }
}

export interface ApiClientOptions {
    baseURL?: string;
    requestId?: () => string;
    fetchImpl?: typeof fetch;
}

export class ApiClient {
    private baseURL: string;
    private requestId: () => string;
    private fetchImpl: typeof fetch;

    constructor(opts: ApiClientOptions = {}) {
        this.baseURL =
            opts.baseURL ?? import.meta.env.VITE_API_BASE_URL ?? "/api/v1";
        this.requestId =
            opts.requestId ??
            (() => globalThis.crypto.randomUUID());
        this.fetchImpl = opts.fetchImpl ?? globalThis.fetch.bind(globalThis);
    }

    async get<T>(path: string, init?: RequestInit): Promise<T> {
        return this.request<T>("GET", path, undefined, init);
    }

    async post<T>(path: string, body?: unknown, init?: RequestInit): Promise<T> {
        return this.request<T>("POST", path, body, init);
    }

    async put<T>(path: string, body?: unknown, init?: RequestInit): Promise<T> {
        return this.request<T>("PUT", path, body, init);
    }

    async delete<T>(path: string, init?: RequestInit): Promise<T> {
        return this.request<T>("DELETE", path, undefined, init);
    }

    private async request<T>(
        method: string,
        path: string,
        body?: unknown,
        init?: RequestInit,
    ): Promise<T> {
        const url = path.startsWith("http") ? path : `${this.baseURL}${path}`;
        const headers = new Headers(init?.headers);
        headers.set("X-Request-ID", this.requestId());
        if (body !== undefined && !headers.has("Content-Type")) {
            headers.set("Content-Type", "application/json");
        }
        const resp = await this.fetchImpl(url, {
            ...init,
            method,
            credentials: "include",
            headers,
            body: body !== undefined ? JSON.stringify(body) : undefined,
        });
        if (resp.status === 204) {
            return undefined as T;
        }
        const text = await resp.text();
        const json = text ? safeJsonParse(text) : undefined;
		if (!resp.ok) {
			const envelope = isRecord(json) ? json : undefined;
			const errEnv: ApiErrorType = isApiError(envelope?.error)
				? envelope.error
                : {
                      code: ErrorCode.INTERNAL,
                      message: resp.statusText || "request failed",
                      requestId: headers.get("X-Request-ID") ?? "",
                  };
            throw new ApiClientError(errEnv, resp.status);
        }
        return json as T;
    }
}

function safeJsonParse(text: string): unknown {
    try {
        return JSON.parse(text);
    } catch {
        return undefined;
    }
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null;
}

function isApiError(value: unknown): value is ApiErrorType {
	return (
		isRecord(value) &&
		typeof value.code === "string" &&
		typeof value.message === "string" &&
		typeof value.requestId === "string"
	);
}

export const apiClient = new ApiClient();

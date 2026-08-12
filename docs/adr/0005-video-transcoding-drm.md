# ADR 0005: 视频转码与 DRM

- **状态**：草案（基于 OQ-005）
- **日期**：2026-08-08

## 决策（MVP）

- **私有 S3 + CloudFront OAC + signed URL/cookie**（TTL ≤ 5 分钟）。
- **不引入 MediaConvert / MediaPackage**；上传源文件直接播放（mp4/webm）。
- 预留 `media_assets.scan_status` 字段，待后续接异步转码 + 病毒扫描。

## 限制

- 大文件首播延迟较高（>100MB mp4）。
- 无 HLS / DRM，盗链风险靠短 TTL + 私有桶降低。

## 后续

- 引入 MediaConvert 转 HLS + CloudFront 分片签名。
- 可选 Widevine / FairPlay DRM（成本 + UX 权衡）。
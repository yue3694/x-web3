-- 0001_roles.sql: 角色与权限种子数据。F01 + 预留。
-- 顺序：roles → permissions → role_permissions

BEGIN;

-- ============================================================
-- roles
-- ============================================================
INSERT INTO roles (code, description) VALUES
  ('student',     'Default learner; can browse, buy, learn, claim certificates'),
  ('teacher',     'Course author; can create/edit own courses and media'),
  ('super_admin', 'Hidden role; full operational control')
ON CONFLICT (code) DO NOTHING;

-- ============================================================
-- permissions
-- ============================================================
INSERT INTO permissions (code, description) VALUES
  ('COURSE_CREATE',          'Create course draft'),
  ('COURSE_EDIT',            'Edit course draft (subject to object-level check)'),
  ('COURSE_APPROVE',         'Approve / reject / archive courses'),
  ('ORDER_CREATE',           'Create purchase intent'),
  ('LESSON_PROGRESS_WRITE',  'Write lesson progress for enrolled courses'),
  ('CERTIFICATE_READ',       'Read own certificates'),
  ('COMMENT_MODERATE',       'Moderate / delete comments'),
  ('MEDIA_UPLOAD',           'Request presigned upload and finalize media'),
  ('ROLE_MANAGE',            'Grant / revoke roles'),
  ('SYSTEM_ADMIN',           'Generic admin access; gating dangerous endpoints'),
  ('CHAIN_SYNC_REPLAY',      'Replay chain events within a safe block range'),
  ('CERTIFICATE_RETRY',      'Retry failed certificate mint jobs')
ON CONFLICT (code) DO NOTHING;

-- ============================================================
-- role_permissions
-- ============================================================
-- student
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.code = 'student'
  AND p.code IN ('ORDER_CREATE', 'LESSON_PROGRESS_WRITE', 'CERTIFICATE_READ', 'MEDIA_UPLOAD')
ON CONFLICT DO NOTHING;

-- teacher
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.code = 'teacher'
  AND p.code IN (
    'COURSE_CREATE', 'COURSE_EDIT', 'MEDIA_UPLOAD',
    'ORDER_CREATE', 'LESSON_PROGRESS_WRITE', 'CERTIFICATE_READ'
  )
ON CONFLICT DO NOTHING;

-- super_admin：获得所有权限（业务代码层面 super_admin 已通配；这里冗余注册方便审计）
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.code = 'super_admin'
ON CONFLICT DO NOTHING;

COMMIT;
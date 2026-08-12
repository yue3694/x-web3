-- 0009_cert_jobs.down.sql
BEGIN;

DROP TRIGGER IF EXISTS certificate_jobs_touch_updated_at ON certificate_jobs;
DROP TRIGGER IF EXISTS certificates_touch_updated_at ON certificates;
DROP TABLE IF EXISTS certificate_jobs;
DROP TABLE IF EXISTS certificates;
DROP TABLE IF EXISTS course_completions;
DROP TABLE IF EXISTS lesson_progress;

COMMIT;
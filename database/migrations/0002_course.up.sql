BEGIN;

CREATE TABLE course_categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_id uuid REFERENCES course_categories(id) ON DELETE RESTRICT,
    slug text NOT NULL UNIQUE,
    title text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE courses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    teacher_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    slug text NOT NULL UNIQUE,
    title text NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    current_version integer NOT NULL DEFAULT 1,
    price_minor bigint NOT NULL DEFAULT 0,
    currency text NOT NULL DEFAULT 'USD',
    published_at timestamptz,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT courses_status_chk CHECK (status IN ('draft','pending_review','published','archived')),
    CONSTRAINT courses_title_chk CHECK (char_length(title) BETWEEN 1 AND 160),
    CONSTRAINT courses_price_chk CHECK (price_minor >= 0),
    CONSTRAINT courses_version_chk CHECK (current_version > 0)
);
CREATE INDEX courses_teacher_idx ON courses(teacher_id, created_at DESC);
CREATE INDEX courses_catalog_idx ON courses(published_at DESC, id DESC) WHERE status = 'published' AND deleted_at IS NULL;

CREATE TABLE course_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    version integer NOT NULL,
    description text NOT NULL DEFAULT '',
    completion_rule jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(course_id, version)
);

CREATE TABLE media_assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    s3_key text NOT NULL UNIQUE,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    scan_status text NOT NULL DEFAULT 'pending',
    checksum_sha256 text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT media_size_chk CHECK (size_bytes > 0),
    CONSTRAINT media_status_chk CHECK (status IN ('draft','ready','failed'))
);

CREATE TABLE chapters (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    course_version_id uuid NOT NULL REFERENCES course_versions(id) ON DELETE CASCADE,
    position integer NOT NULL,
    title text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(course_version_id, position),
    CONSTRAINT chapters_position_chk CHECK (position >= 0)
);

CREATE TABLE lessons (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    chapter_id uuid NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    position integer NOT NULL,
    title text NOT NULL,
    required boolean NOT NULL DEFAULT true,
    media_asset_id uuid REFERENCES media_assets(id) ON DELETE RESTRICT,
    duration_seconds integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(chapter_id, position),
    CONSTRAINT lessons_position_chk CHECK (position >= 0),
    CONSTRAINT lessons_duration_chk CHECK (duration_seconds >= 0)
);

CREATE TABLE course_category_map (
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    category_id uuid NOT NULL REFERENCES course_categories(id) ON DELETE RESTRICT,
    PRIMARY KEY(course_id, category_id)
);

CREATE TABLE comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body text NOT NULL,
    moderation_status text NOT NULL DEFAULT 'pending',
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT comments_body_chk CHECK (char_length(body) BETWEEN 1 AND 2000),
    CONSTRAINT comments_moderation_chk CHECK (moderation_status IN ('pending','approved','rejected'))
);
CREATE INDEX comments_course_idx ON comments(course_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE course_audit_logs (
    id bigserial PRIMARY KEY,
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE RESTRICT,
    from_status text NOT NULL,
    to_status text NOT NULL,
    actor_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX course_audit_course_idx ON course_audit_logs(course_id, created_at DESC);

CREATE TRIGGER courses_touch_updated_at BEFORE UPDATE ON courses FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER course_versions_touch_updated_at BEFORE UPDATE ON course_versions FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER categories_touch_updated_at BEFORE UPDATE ON course_categories FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER media_assets_touch_updated_at BEFORE UPDATE ON media_assets FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER chapters_touch_updated_at BEFORE UPDATE ON chapters FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER lessons_touch_updated_at BEFORE UPDATE ON lessons FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
CREATE TRIGGER comments_touch_updated_at BEFORE UPDATE ON comments FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

COMMIT;

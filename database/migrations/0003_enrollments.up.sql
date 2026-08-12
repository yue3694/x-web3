BEGIN;

CREATE TABLE enrollments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    source text NOT NULL DEFAULT 'order',
    enrolled_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, course_id),
    CONSTRAINT enrollments_source_chk CHECK (source IN ('order','admin_grant','seed'))
);
CREATE INDEX enrollments_user_idx ON enrollments(user_id, enrolled_at DESC);
CREATE INDEX enrollments_course_idx ON enrollments(course_id);

COMMIT;

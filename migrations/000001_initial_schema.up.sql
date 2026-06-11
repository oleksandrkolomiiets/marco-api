CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE users (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    clerk_id       VARCHAR(255) UNIQUE NOT NULL,
    email          VARCHAR(255) UNIQUE NOT NULL,
    display_name   VARCHAR(255),
    skill_level    VARCHAR(20)  CHECK (skill_level IN ('beginner', 'intermediate', 'advanced')),
    dominant_hand  VARCHAR(5)   CHECK (dominant_hand IN ('left', 'right')),
    court_side     VARCHAR(6)   CHECK (court_side IN ('left', 'right', 'either')),
    play_frequency VARCHAR(20),
    goal           VARCHAR(50),
    plan           VARCHAR(20)  NOT NULL DEFAULT 'free' CHECK (plan IN ('free', 'premium', 'coach')),
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE lessons (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    slug             VARCHAR(100) UNIQUE NOT NULL,
    title            VARCHAR(255) NOT NULL,
    level            VARCHAR(20)  NOT NULL CHECK (level IN ('beginner', 'intermediate', 'advanced')),
    order_index      INT          NOT NULL,
    video_url        TEXT,
    thumbnail_url    TEXT,
    duration_seconds INT,
    cue_points       JSONB        NOT NULL DEFAULT '[]',
    common_mistake   TEXT,
    drill            TEXT,
    is_free          BOOLEAN      NOT NULL DEFAULT false,
    published        BOOLEAN      NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE user_lesson_progress (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lesson_id  UUID        NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,
    status     VARCHAR(20) NOT NULL DEFAULT 'viewed' CHECK (status IN ('viewed', 'learned', 'mastered')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, lesson_id)
);

CREATE TRIGGER user_lesson_progress_set_updated_at
    BEFORE UPDATE ON user_lesson_progress
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE chat_sessions (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE chat_messages (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID        NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    role       VARCHAR(20) NOT NULL CHECK (role IN ('user', 'assistant')),
    content    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- clerk_id and email UNIQUE constraints already create implicit indexes on users.
-- UNIQUE(user_id, lesson_id) already covers user_id-leading lookups on user_lesson_progress.

CREATE INDEX idx_chat_sessions_user_id              ON chat_sessions (user_id);
CREATE INDEX idx_user_lesson_progress_lesson_id     ON user_lesson_progress (lesson_id);
CREATE INDEX idx_chat_messages_session_created      ON chat_messages (session_id, created_at);
CREATE INDEX idx_lessons_level_order                ON lessons (level, order_index);
CREATE INDEX idx_lessons_published_level_order      ON lessons (level, order_index) WHERE published = true;

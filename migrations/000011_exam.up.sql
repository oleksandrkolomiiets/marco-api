CREATE TABLE exam_questions (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        VARCHAR(100) UNIQUE NOT NULL,
    order_index INT          UNIQUE NOT NULL,
    category    VARCHAR(80)  NOT NULL,
    prompt      TEXT         NOT NULL,
    explanation TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE exam_options (
    id          UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID    NOT NULL REFERENCES exam_questions(id) ON DELETE CASCADE,
    order_index INT     NOT NULL,
    text        TEXT    NOT NULL,
    is_correct  BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (question_id, order_index)
);

CREATE INDEX idx_exam_options_question ON exam_options (question_id);

CREATE TABLE exam_attempts (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    score        INT         NOT NULL,
    total        INT         NOT NULL,
    passed       BOOLEAN     NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_exam_attempts_user ON exam_attempts (user_id, completed_at DESC);

CREATE TABLE exam_attempt_answers (
    id                 UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    attempt_id         UUID    NOT NULL REFERENCES exam_attempts(id) ON DELETE CASCADE,
    question_id        UUID    NOT NULL REFERENCES exam_questions(id) ON DELETE RESTRICT,
    selected_option_id UUID    REFERENCES exam_options(id) ON DELETE SET NULL,
    is_correct         BOOLEAN NOT NULL,
    UNIQUE (attempt_id, question_id)
);

CREATE INDEX idx_exam_answers_attempt ON exam_attempt_answers (attempt_id);

CREATE TABLE messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role VARCHAR(16) NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
  content TEXT NOT NULL,
  feedback_score SMALLINT NOT NULL DEFAULT 0 CHECK (feedback_score IN (-1, 0, 1)),
  lesson_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_messages_user_id_created_at ON messages (user_id, created_at DESC);
CREATE INDEX idx_messages_user_id_feedback ON messages (user_id) WHERE feedback_score <> 0;

COMMENT ON TABLE messages IS 'Chat messages between users and Marco the AI coach';
COMMENT ON COLUMN messages.role IS 'Source of the message: user input, assistant (Marco), or system metadata';
COMMENT ON COLUMN messages.feedback_score IS 'User feedback: -1 thumbs down, 0 no feedback, 1 thumbs up';
COMMENT ON COLUMN messages.lesson_refs IS 'Parsed [LESSON_REF: id | title] tokens from assistant messages, used by mobile client to render inline lesson cards';

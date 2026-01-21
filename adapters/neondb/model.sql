CREATE TABLE media (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id TEXT NOT NULL,         -- Clerk user ID
    type TEXT NOT NULL,            -- image | video | audio | text
    name TEXT NOT NULL,

    original_url TEXT NOT NULL,
    processed_url TEXT,

    format TEXT,
    size_bytes BIGINT,

    width INT,
    height INT,
    duration_seconds INT,          -- for video/audio later

    status TEXT DEFAULT 'uploaded', -- uploaded | processing | ready | failed

    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_media_user ON media(user_id);
CREATE INDEX idx_media_type ON media(type);

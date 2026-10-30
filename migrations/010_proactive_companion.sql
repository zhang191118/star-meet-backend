-- Stage 7: user and character-level proactive companion settings and events.
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS proactive_quiet_start VARCHAR(5) DEFAULT '22:30';
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS proactive_quiet_end VARCHAR(5) DEFAULT '08:00';
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS proactive_daily_limit INT DEFAULT 1;
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS proactive_only_when_away BOOLEAN DEFAULT TRUE;
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS proactive_preview_mode VARCHAR(16) DEFAULT 'name_only';
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS promise_reminder_enabled BOOLEAN DEFAULT TRUE;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS character_id VARCHAR(64);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS deep_link VARCHAR(255);
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS proactive_event_id VARCHAR(64);

CREATE TABLE IF NOT EXISTS proactive_events (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    event_id VARCHAR(64) NOT NULL UNIQUE,
    user_id VARCHAR(64) NOT NULL,
    character_id VARCHAR(64) NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    source_memory_id VARCHAR(64) DEFAULT '',
    trigger_at DATETIME(3) NOT NULL,
    expires_at DATETIME(3) NULL,
    dedupe_key VARCHAR(160) NOT NULL UNIQUE,
    reason VARCHAR(255) DEFAULT '',
    status VARCHAR(16) DEFAULT 'pending',
    processing_at DATETIME(3) NULL,
    assistant_message_id VARCHAR(64) NULL,
    generated_at DATETIME(3) NULL,
    attempt_count INT DEFAULT 0,
    last_error VARCHAR(500) DEFAULT '',
    sent_at DATETIME(3) NULL
);

CREATE TABLE IF NOT EXISTS user_character_notification_settings (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    character_id VARCHAR(64) NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    daily_limit INT DEFAULT 1,
    muted_until DATETIME(3) NULL,
    UNIQUE KEY idx_user_character_notification_settings (user_id, character_id)
);

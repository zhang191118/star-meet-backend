-- Stage 6: persistent character emotion with bounded intensity.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS character_emotion VARCHAR(32) DEFAULT '平静';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS emotion_intensity INT DEFAULT 1;

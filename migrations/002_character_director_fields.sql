-- 星遇角色系统本土化改造迁移
-- 基于 Crushie 角色卡模式扩展 character 和 session 表

-- ===== characters 表新增字段 =====
ALTER TABLE characters ADD COLUMN IF NOT EXISTS comment TEXT DEFAULT '';
ALTER TABLE characters ADD COLUMN IF NOT EXISTS narrative TEXT DEFAULT '';
ALTER TABLE characters ADD COLUMN IF NOT EXISTS basic_setting TEXT DEFAULT '';
ALTER TABLE characters ADD COLUMN IF NOT EXISTS example TEXT DEFAULT '';
ALTER TABLE characters ADD COLUMN IF NOT EXISTS first_situation TEXT DEFAULT '';
ALTER TABLE characters ADD COLUMN IF NOT EXISTS first_words TEXT DEFAULT '';
ALTER TABLE characters ADD COLUMN IF NOT EXISTS character_type INT DEFAULT 0;
ALTER TABLE characters ADD COLUMN IF NOT EXISTS tags_json TEXT DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_characters_type ON characters(character_type);

-- ===== sessions 表新增字段 =====
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS director_state TEXT DEFAULT '';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS relationship_stage VARCHAR(20) DEFAULT '初識';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS affection_score INT DEFAULT 0;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS message_count INT DEFAULT 0;

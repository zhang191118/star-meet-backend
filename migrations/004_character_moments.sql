-- 角色动态表（朋友圈式内容，由 scripts/publish_moments.py 每日生成）
-- 说明：主程序启动时已通过 GORM AutoMigrate 建表，此 SQL 供手动执行/核对结构使用

CREATE TABLE IF NOT EXISTS character_moments (
  id            BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  created_at    DATETIME(3) NULL,
  updated_at    DATETIME(3) NULL,
  deleted_at    DATETIME(3) NULL,
  character_id  VARCHAR(64)  NOT NULL,
  content       TEXT         NOT NULL,
  image_urls    JSON         NULL,
  topic_tags    VARCHAR(255) DEFAULT '',
  like_count    INT          DEFAULT 0,
  status        INT          NULL COMMENT '1=正常 0=下架',
  publish_day   VARCHAR(10)  NOT NULL COMMENT 'YYYY-MM-DD，用于每角色每天一条',
  published_at  DATETIME(3)  NULL,
  UNIQUE KEY idx_moments_char_day (character_id, publish_day),
  KEY idx_moments_feed (status, published_at),
  KEY idx_character_moments_deleted_at (deleted_at)
);

-- Runtime configuration template. Replace placeholder values before importing.
-- MySQL usage:
--   mysql -u starmeet -p star_meet < scripts/runtime_configs.example.sql

INSERT INTO runtime_configs (config_key, `value`, `sensitive`, description, created_at, updated_at)
VALUES
  ('jwt.secret', 'replace-with-long-random-secret', true, 'JWT signing secret', NOW(), NOW()),
  ('admin.config_token', 'replace-with-admin-config-token', true, 'Token for /api/v1/admin/configs', NOW(), NOW()),
  ('email.host', 'smtp.example.com', false, 'SMTP host', NOW(), NOW()),
  ('email.port', '587', false, 'SMTP port', NOW(), NOW()),
  ('email.username', 'noreply@example.com', true, 'SMTP username', NOW(), NOW()),
  ('email.password', 'replace-with-smtp-password', true, 'SMTP password or authorization code', NOW(), NOW()),
  ('email.from_name', '星遇 StarMeet', false, 'Email sender display name', NOW(), NOW()),
  ('email.from_email', 'noreply@example.com', false, 'Email sender address', NOW(), NOW()),
  ('oss.endpoint', 'oss-cn-hangzhou.aliyuncs.com', false, 'Aliyun OSS endpoint', NOW(), NOW()),
  ('oss.access_key_id', 'replace-with-oss-access-key-id', true, 'Aliyun OSS access key ID', NOW(), NOW()),
  ('oss.access_key_secret', 'replace-with-oss-access-key-secret', true, 'Aliyun OSS access key secret', NOW(), NOW()),
  ('oss.bucket_name', 'star-meet', false, 'Aliyun OSS bucket', NOW(), NOW()),
  ('aliyun.qwen.api_key', 'replace-with-dashscope-api-key', true, 'DashScope/Qwen API key', NOW(), NOW()),
  ('mem0.host', 'https://api.mem0.ai', false, 'Mem0 API host', NOW(), NOW()),
  ('mem0.api_key', 'replace-with-mem0-api-key', true, 'Mem0 API key', NOW(), NOW()),
  ('minimax.api_key', 'replace-with-minimax-api-key', true, 'MiniMax API key', NOW(), NOW()),
  ('openai_compat.base_url', 'https://ai98pro.xyz', false, 'OpenAI-compatible chat API base URL', NOW(), NOW()),
  ('openai_compat.api_key', 'replace-with-openai-compatible-api-key', true, 'OpenAI-compatible chat API key', NOW(), NOW()),
  ('openai_compat.model', 'grok-4.3-fast', false, 'OpenAI-compatible primary chat model', NOW(), NOW()),
  ('deepseek.api_key', 'replace-with-deepseek-api-key', true, 'DeepSeek API key', NOW(), NOW()),
  ('gemini.api_key', 'replace-with-gemini-api-key', true, 'Gemini API key', NOW(), NOW()),
  ('gemini.model', 'gemini-2.0-flash', false, 'Gemini model name', NOW(), NOW()),
  ('qdrant.host', 'http://localhost:6333', false, 'Qdrant endpoint', NOW(), NOW())
ON DUPLICATE KEY UPDATE
  `value` = VALUES(`value`),
  `sensitive` = VALUES(`sensitive`),
  description = VALUES(description),
  updated_at = NOW();

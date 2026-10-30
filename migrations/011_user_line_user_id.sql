-- LINE login: add line_user_id to users.
-- safeAutoMigrateModels intentionally skips existing tables, so new columns
-- on legacy tables must be added manually.
ALTER TABLE users ADD COLUMN line_user_id varchar(64) NOT NULL DEFAULT '';
CREATE INDEX idx_users_line_user_id ON users (line_user_id);

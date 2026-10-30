-- Stage 3: bounded rolling context summary for character sessions.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS context_summary TEXT DEFAULT '';

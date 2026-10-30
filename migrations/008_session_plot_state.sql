-- Stage 4: persisted plot state machine snapshot for each character session.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS plot_state VARCHAR(32) DEFAULT 'opening';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS plot_beat INT DEFAULT 0;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS plot_threads TEXT DEFAULT '';

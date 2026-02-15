DROP TRIGGER IF EXISTS update_submissions_updated_at ON submissions;
DROP TRIGGER IF EXISTS update_users_updated_at ON users;
DROP FUNCTION IF EXISTS update_updated_at_column();

DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS grades;
DROP TABLE IF EXISTS submissions;
DROP TABLE IF EXISTS users;

DROP EXTENSION IF EXISTS "pgcrypto";
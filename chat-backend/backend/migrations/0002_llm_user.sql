-- Seed the system LLM user used as the recipient of "chat with the LLM to book" messages.
-- The password hash is a real bcrypt of an unguessable value; this account cannot be logged into.
INSERT INTO users (username, password_hash)
VALUES ('llm', '$2a$10$17sPfaf3rFqHWzf/e5JdbeLP7Qu/3QCMm9YS.KqNyiO85DfTQKsHu')
ON CONFLICT (username) DO NOTHING;
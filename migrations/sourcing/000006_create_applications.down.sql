DROP TABLE IF EXISTS applications_default;
DROP TABLE IF EXISTS applications;
DROP TABLE IF EXISTS hiring_intent_embeddings;
ALTER TABLE candidates DROP COLUMN IF EXISTS profile_embedding;

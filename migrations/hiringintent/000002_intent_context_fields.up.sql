-- Add organizational context fields the Figr design surfaces but the
-- aggregate didn't capture: why the role exists (reason), the team or
-- squad the hire joins (team), and the hiring manager (reports_to).
-- All are optional free text — empty string when not provided. Capped at
-- 500 chars at the domain layer; the column is TEXT for headroom.
ALTER TABLE hiring_intents
    ADD COLUMN reason     TEXT NOT NULL DEFAULT '',
    ADD COLUMN team       TEXT NOT NULL DEFAULT '',
    ADD COLUMN reports_to TEXT NOT NULL DEFAULT '';

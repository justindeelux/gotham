-- +goose Up
-- Profile display name (PF-1): optional free text, 1-64 characters. NULL means
-- unset; the CHECK admits NULL (unknown is not false) and rejects the empty
-- string, so the application only needs the same bounds check.
ALTER TABLE users
    ADD COLUMN display_name text NULL CHECK (char_length(display_name) BETWEEN 1 AND 64);

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS display_name;

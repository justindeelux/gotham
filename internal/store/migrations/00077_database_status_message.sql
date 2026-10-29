-- +goose Up
-- Async database provisioning (GOTHAM-111): a short, redacted reason stored
-- when a create fails so the detail page can explain the error. Cleared when
-- the database reaches running.
ALTER TABLE databases ADD COLUMN status_message text;

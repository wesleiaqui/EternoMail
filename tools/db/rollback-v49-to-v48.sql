-- Roll back credential storage source markers (v49) to the v48 schema.
-- The markers are metadata only; encrypted fallback ciphertext and all other
-- account and contact-source data remain unchanged.
BEGIN;

ALTER TABLE accounts DROP COLUMN password_storage;
ALTER TABLE accounts DROP COLUMN smtp_password_storage;
ALTER TABLE contact_sources DROP COLUMN password_storage;
DELETE FROM migrations WHERE version >= 49;

COMMIT;

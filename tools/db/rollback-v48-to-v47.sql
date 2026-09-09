-- Roll back certificate host-scoped trust (v48) to the v47 schema.
-- v47 can store each fingerprint only once, so when a certificate was trusted
-- for several hosts this keeps the most recently accepted grant.
BEGIN;

CREATE TABLE trusted_certificates_v47 (
	id TEXT PRIMARY KEY,
	fingerprint TEXT NOT NULL UNIQUE,
	host TEXT NOT NULL DEFAULT '',
	subject TEXT NOT NULL,
	issuer TEXT NOT NULL,
	not_before DATETIME,
	not_after DATETIME,
	accepted_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR REPLACE INTO trusted_certificates_v47
	(id, fingerprint, host, subject, issuer, not_before, not_after, accepted_at)
SELECT id, fingerprint, host, subject, issuer, not_before, not_after, accepted_at
FROM trusted_certificates
ORDER BY accepted_at ASC;

DROP TABLE trusted_certificates;
ALTER TABLE trusted_certificates_v47 RENAME TO trusted_certificates;
DELETE FROM migrations WHERE version >= 48;

COMMIT;

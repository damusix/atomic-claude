-- Reference: TrackingStatus_Allowed
-- Source: tmp/llm-memory-db.pseudo  (REFERENCE TABLES section)

CREATE TABLE IF NOT EXISTS "TrackingStatus_Allowed" (
    from_status         VARCHAR(32) NOT NULL,
    to_status           VARCHAR(32) NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (from_status, to_status),
    FOREIGN KEY (from_status) REFERENCES "TrackingStatus" (tracking_status) ON DELETE CASCADE,
    FOREIGN KEY (to_status)   REFERENCES "TrackingStatus" (tracking_status) ON DELETE CASCADE
);

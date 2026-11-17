-- S58 T1: the supervision plane's six tables. attention_ack is the append-only memory of what a person has already seen (ack/snooze/unack — the latest row per subject_key is the truth, SV3); notify_event_class is the closed class table the notify resolver reads (kind+variant, tone mirrored from protocol.NotifyTone and the app's NotifyTone — all four values, lock-tested); notify_channel is an outbound webhook as an authority record — created DISABLED and its creator kind is CHECKed to 'user' alone (SV8 lock 3: no agent, no import, no cli ever owns a channel row; the secret never lives here, only the secret_ref); notify_subscription scopes a channel to a root ('' = every root, the S40 convention — no FK to component so a subscription survives tree changes) with all-or-none quiet windows and a rate ceiling; notify_delivery is the append-only outcome log (no FK to the channel: history outlives the channel, SV12); notify_change is the append-only audit of every channel/subscription/class write (the setting_event/policy_change precedent). The three append-only tables are refused UPDATE and DELETE by trigger.

CREATE TABLE attention_ack (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  subject_key TEXT NOT NULL,
  op          TEXT NOT NULL CHECK (op IN ('ack','snooze','unack')),
  until       TEXT,
  actor_kind  TEXT NOT NULL,
  actor_id    TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  CHECK ((op='snooze') = (until IS NOT NULL))
);
CREATE INDEX idx_attention_ack_subject ON attention_ack (subject_key, id);

CREATE TABLE notify_event_class (
  kind           TEXT NOT NULL,
  variant        TEXT NOT NULL DEFAULT '',
  tone           TEXT NOT NULL CHECK (tone IN ('info','success','warning','danger')),
  title_template TEXT NOT NULL,
  body_template  TEXT NOT NULL DEFAULT '',
  enabled        INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  origin         TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('system','user','import')),
  version        INTEGER NOT NULL DEFAULT 1,
  updated_at     TEXT NOT NULL,
  PRIMARY KEY (kind, variant)
);

CREATE TABLE notify_channel (
  id              TEXT PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN ('webhook')),
  name            TEXT NOT NULL,
  enabled         INTEGER NOT NULL DEFAULT 0,
  url             TEXT NOT NULL,
  secret_ref      TEXT NOT NULL,
  allow_private   INTEGER NOT NULL DEFAULT 0 CHECK (allow_private IN (0,1)),
  created_by_kind TEXT NOT NULL CHECK (created_by_kind = 'user'),
  created_by_id   TEXT NOT NULL,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  archived_at     TEXT,
  archive_reason  TEXT
);

CREATE TABLE notify_subscription (
  channel_id   TEXT NOT NULL REFERENCES notify_channel(id),
  root_id      TEXT NOT NULL DEFAULT '',
  min_severity TEXT NOT NULL,
  kinds        TEXT,
  quiet_start  TEXT,
  quiet_end    TEXT,
  quiet_zone   TEXT,
  max_per_hour INTEGER NOT NULL DEFAULT 30 CHECK (max_per_hour BETWEEN 1 AND 1000),
  PRIMARY KEY (channel_id, root_id),
  CHECK ((quiet_start IS NULL) = (quiet_end IS NULL) AND (quiet_start IS NULL) = (quiet_zone IS NULL))
);

CREATE TABLE notify_delivery (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  channel_id    TEXT NOT NULL,
  event_seq     INTEGER,
  attention_key TEXT,
  outcome       TEXT NOT NULL CHECK (outcome IN ('sent','failed','suppressed-quiet','suppressed-rate','suppressed-dedup','suppressed-disabled','suppressed-overflow')),
  error_kind    TEXT CHECK (error_kind IS NULL OR error_kind IN ('transport','timeout','status','refused-address','secret-missing','url-invalid')),
  http_status   INTEGER,
  attempts      INTEGER NOT NULL DEFAULT 0,
  at            TEXT NOT NULL
);
CREATE INDEX idx_notify_delivery_channel ON notify_delivery (channel_id, at);

CREATE TABLE notify_change (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  subject_kind TEXT NOT NULL CHECK (subject_kind IN ('channel','subscription','class')),
  subject_id   TEXT NOT NULL,
  op           TEXT NOT NULL,
  actor_kind   TEXT NOT NULL,
  actor_id     TEXT NOT NULL,
  occurred_at  TEXT NOT NULL,
  summary      TEXT NOT NULL
);

CREATE TRIGGER attention_ack_no_update BEFORE UPDATE ON attention_ack
BEGIN SELECT RAISE(ABORT, 'attention_ack is append-only: a seen-marker is never rewritten (S58 SV3)'); END;
CREATE TRIGGER attention_ack_no_delete BEFORE DELETE ON attention_ack
BEGIN SELECT RAISE(ABORT, 'attention_ack is append-only: a seen-marker is never deleted (S58 SV3)'); END;
CREATE TRIGGER notify_delivery_no_update BEFORE UPDATE ON notify_delivery
BEGIN SELECT RAISE(ABORT, 'notify_delivery is append-only: a delivery outcome is never rewritten (S58 SV12)'); END;
CREATE TRIGGER notify_delivery_no_delete BEFORE DELETE ON notify_delivery
BEGIN SELECT RAISE(ABORT, 'notify_delivery is append-only: a delivery outcome is never deleted (S58 SV12)'); END;
CREATE TRIGGER notify_change_no_update BEFORE UPDATE ON notify_change
BEGIN SELECT RAISE(ABORT, 'notify_change is append-only: an audit row is never rewritten (S58 SV8)'); END;
CREATE TRIGGER notify_change_no_delete BEFORE DELETE ON notify_change
BEGIN SELECT RAISE(ABORT, 'notify_change is append-only: an audit row is never deleted (S58 SV8)'); END;

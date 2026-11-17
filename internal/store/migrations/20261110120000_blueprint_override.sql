-- A person's override of one allowed field on one lane of a blueprint. Shipped (system) blueprints are locked: their bytes never change, so what a person may still choose lives here, beside them, keyed by the blueprint name and the lane id. `field` is a closed set the daemon declares (today only `route`, the provider/model route a model-step lane runs on); a row exists only while the override does — clearing deletes it. Global to the daemon.
CREATE TABLE blueprint_override (
  blueprint  TEXT NOT NULL CHECK (length(blueprint) BETWEEN 1 AND 64),
  lane_id    TEXT NOT NULL CHECK (length(lane_id) BETWEEN 1 AND 128),
  field      TEXT NOT NULL CHECK (field IN ('route')),
  value      TEXT NOT NULL CHECK (length(value) BETWEEN 1 AND 128),
  updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL CHECK (length(updated_by) BETWEEN 1 AND 256),
  PRIMARY KEY (blueprint, lane_id, field)
);

DROP TABLE IF EXISTS usage_event;

ALTER TABLE promotion_roi_record DROP COLUMN IF EXISTS replaces_campaign_id;
ALTER TABLE promotion_roi_record DROP COLUMN IF EXISTS origin;

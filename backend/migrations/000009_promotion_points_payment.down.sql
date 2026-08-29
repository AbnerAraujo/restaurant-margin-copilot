ALTER TABLE promotion_roi_record DROP CONSTRAINT IF EXISTS points_spent_matches_payment_method;
ALTER TABLE promotion_roi_record DROP COLUMN IF EXISTS points_spent;
ALTER TABLE promotion_roi_record DROP COLUMN IF EXISTS payment_method;

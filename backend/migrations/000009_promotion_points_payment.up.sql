-- Lets an owner fund a promotion's spend with earned Steward points instead
-- of money (POST /api/promotions). spend still records the real dollar
-- amount the campaign cost — ROI math never changes based on how it was
-- funded — payment_method and points_spent are provenance on TOP of that
-- figure, the same layering origin/replaces_campaign_id already use.
ALTER TABLE promotion_roi_record
    ADD COLUMN payment_method TEXT NOT NULL DEFAULT 'money'
        CHECK (payment_method IN ('money', 'points'));

ALTER TABLE promotion_roi_record
    ADD COLUMN points_spent INTEGER;

-- Structurally impossible to have one without the other: a 'points' row
-- must say how many points, and a 'money' row must not carry a stray points
-- figure that would double-count against the earned balance.
ALTER TABLE promotion_roi_record
    ADD CONSTRAINT points_spent_matches_payment_method CHECK (
        (payment_method = 'points' AND points_spent IS NOT NULL AND points_spent > 0)
        OR (payment_method = 'money' AND points_spent IS NULL)
    );

COMMENT ON COLUMN promotion_roi_record.payment_method IS
    'How this campaign''s spend was funded: money (default, every row before '
    'this migration) or points (redeemed from the owner''s earned Steward '
    'points balance, internal/badges). Never decided by the model — set by '
    'the owner''s explicit choice at POST /api/promotions, verified '
    'server-side against the real earned-minus-spent balance before insert.';
COMMENT ON COLUMN promotion_roi_record.points_spent IS
    'Points redeemed to cover this campaign''s spend, at the fixed, '
    'disclosed conversion rate internal/badges.CentsPerPoint defines. NULL '
    'whenever payment_method is money. Summed across every points-paid row '
    '(regardless of period) to compute how much of the owner''s earned '
    'balance is already committed — see SumPointsSpentOnPromotions.';

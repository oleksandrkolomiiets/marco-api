-- Puts the column back with its original shape and check, matching
-- 000012_curriculum_v2. The values themselves are gone: dropping a column
-- discards its data, and these were authored figures rather than anything
-- derivable, so a down migration cannot reconstruct them. Re-seed from the
-- curriculum file if you need them back — and note the seeder no longer writes
-- this column, so that would need reverting too.
ALTER TABLE lessons
    ADD COLUMN common_mistake_pct SMALLINT CHECK (common_mistake_pct BETWEEN 0 AND 100);

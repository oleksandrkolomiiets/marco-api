-- lessons.common_mistake_pct held a per-lesson figure — 33 through 62 across
-- the curriculum — rendered as "COMMON MISTAKE · 62%" on the lesson screen.
-- Nothing measured it. It was authored alongside the mistake text and read to
-- players as the share of them who make that mistake, which is a claim about a
-- population this app has never observed.
--
-- The mistake text is real coaching and stays. Only the statistic goes.
ALTER TABLE lessons DROP COLUMN common_mistake_pct;

-- Backfill searchable identifiers for pre-existing manual records.
-- Manual movements  (is_projection = false) get MM-######.
-- Manual projections (is_projection = true)  get PM-######.
-- Numbering continues after any codes that already exist, and rows are
-- ordered by creation time so the sequence follows the order they were added.

-- Manual movements → MM-######
WITH seq AS (
	SELECT COALESCE(MAX(CAST(SUBSTRING(reference FROM '^MM-([0-9]+)$') AS INTEGER)), 0) AS n
	FROM   transactions
	WHERE  reference LIKE 'MM-%'
),
numbered AS (
	SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) AS rn
	FROM   transactions
	WHERE  source = 'Manual' AND is_projection = false
	  AND  (reference IS NULL OR reference = '')
)
UPDATE transactions t
SET    reference = 'MM-' || LPAD((seq.n + numbered.rn)::text, 6, '0')
FROM   numbered, seq
WHERE  t.id = numbered.id;

-- Manual projections → PM-######
WITH seq AS (
	SELECT COALESCE(MAX(CAST(SUBSTRING(reference FROM '^PM-([0-9]+)$') AS INTEGER)), 0) AS n
	FROM   transactions
	WHERE  reference LIKE 'PM-%'
),
numbered AS (
	SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) AS rn
	FROM   transactions
	WHERE  source = 'Manual' AND is_projection = true
	  AND  (reference IS NULL OR reference = '')
)
UPDATE transactions t
SET    reference = 'PM-' || LPAD((seq.n + numbered.rn)::text, 6, '0')
FROM   numbered, seq
WHERE  t.id = numbered.id;

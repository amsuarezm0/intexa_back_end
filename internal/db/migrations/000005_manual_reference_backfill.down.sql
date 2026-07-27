-- Reverse the backfill: clear auto-generated MM-/PM- codes from manual records.
UPDATE transactions
SET    reference = NULL
WHERE  source = 'Manual'
  AND  reference ~ '^(MM|PM)-[0-9]+$';

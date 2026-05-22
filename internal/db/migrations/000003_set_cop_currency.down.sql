-- Revert to USD
INSERT INTO settings (key, value)
VALUES ('baseCurrency', '"USD"')
ON CONFLICT (key) DO UPDATE SET value = '"USD"';

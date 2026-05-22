-- Force baseCurrency to COP regardless of previous value
INSERT INTO settings (key, value)
VALUES ('baseCurrency', '"COP"')
ON CONFLICT (key) DO UPDATE SET value = '"COP"';

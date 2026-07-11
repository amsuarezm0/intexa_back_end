-- Revert to the original three-role constraint. Any GESTIÓN users must be
-- reassigned before running this down migration.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('ADMINISTRADOR', 'TESORERÍA', 'CONSULTA'));

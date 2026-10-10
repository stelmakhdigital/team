-- 0019_library_starters (postgres) — P2.13: метка starter на library-элементах
-- (pin-стартеры: готовый набор, который apply'ится в один клик).
ALTER TABLE library_items ADD COLUMN is_starter INTEGER NOT NULL DEFAULT 0;

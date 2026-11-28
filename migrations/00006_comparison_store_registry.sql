-- +goose Up
ALTER TABLE public.comparison_items DROP CONSTRAINT comparison_items_store_check;
ALTER TABLE public.comparison_items ADD CONSTRAINT comparison_items_store_check
    CHECK (store ~ '^[a-z][a-z0-9_]{0,49}$');

-- +goose Down
-- Откат отклоняется, если есть элементы новых магазинов: данные не удаляются.
ALTER TABLE public.comparison_items DROP CONSTRAINT comparison_items_store_check;
ALTER TABLE public.comparison_items ADD CONSTRAINT comparison_items_store_check
    CHECK (store IN ('google_play', 'rustore'));

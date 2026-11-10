-- +goose Up
ALTER TABLE public.store_apps
    ADD COLUMN search_vector tsvector;

CREATE INDEX store_apps_search_idx ON public.store_apps USING GIN (search_vector);

-- Заполнить вектор для уже сохранённых карточек.
UPDATE public.store_apps
SET search_vector =
    to_tsvector('russian', coalesce(metadata->>'name', ''))
    || to_tsvector('simple', coalesce(metadata->>'name', ''));

-- +goose Down
DROP INDEX IF EXISTS public.store_apps_search_idx;
ALTER TABLE public.store_apps DROP COLUMN IF EXISTS search_vector;

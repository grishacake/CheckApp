-- +goose Up
INSERT INTO public.stores (code, name, is_active)
VALUES
    ('google_play', 'Google Play', TRUE),
    ('rustore', 'RuStore', FALSE);

-- +goose Down
DELETE FROM public.stores
WHERE code IN ('google_play', 'rustore');

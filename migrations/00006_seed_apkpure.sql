-- +goose Up
INSERT INTO public.stores (code, name, is_active)
VALUES ('apkpure', 'APKPure', TRUE)
ON CONFLICT (code) DO UPDATE SET is_active = TRUE;

-- +goose Down
UPDATE public.stores SET is_active = FALSE WHERE code = 'apkpure';

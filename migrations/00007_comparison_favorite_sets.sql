-- +goose Up
CREATE TABLE public.comparison_favorite_sets (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
 items JSONB NOT NULL CHECK (jsonb_typeof(items) = 'array' AND jsonb_array_length(items) > 0),
 fingerprint BYTEA NOT NULL,
 saved_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(user_id, fingerprint)
);
CREATE INDEX comparison_favorite_sets_user_date ON public.comparison_favorite_sets(user_id, saved_at DESC, id);
INSERT INTO public.comparison_favorite_sets(user_id,items,fingerprint,saved_at)
SELECT user_id, items, sha256(convert_to(items::text, 'UTF8')), saved_at FROM (
SELECT user_id, jsonb_build_array(
 jsonb_build_object('store','google_play','package_name',google_play),
 jsonb_build_object('store','rustore','package_name',rustore)) AS items, saved_at
FROM public.comparison_favorites) legacy;

-- +goose Down
DROP TABLE public.comparison_favorite_sets;

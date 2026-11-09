-- +goose Up
UPDATE public.stores SET is_active = TRUE WHERE code = 'rustore';

CREATE TABLE public.comparison_history (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    google_play VARCHAR(255) NOT NULL,
    rustore VARCHAR(255) NOT NULL,
    saved_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX comparison_history_user_date ON public.comparison_history (user_id, saved_at DESC, id DESC);

CREATE TABLE public.comparison_favorites (
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    google_play VARCHAR(255) NOT NULL,
    rustore VARCHAR(255) NOT NULL,
    saved_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (user_id, google_play, rustore)
);

-- +goose Down
DROP TABLE public.comparison_favorites;
DROP TABLE public.comparison_history;
UPDATE public.stores SET is_active = FALSE WHERE code = 'rustore';

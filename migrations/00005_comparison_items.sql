-- +goose Up
CREATE TABLE public.comparison_items (
    id BIGSERIAL NOT NULL,
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    store VARCHAR(50) NOT NULL REFERENCES public.stores(code),
    package_name VARCHAR(255) NOT NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (user_id, store, package_name),
    CHECK (store IN ('google_play', 'rustore'))
);
CREATE INDEX comparison_items_user_order ON public.comparison_items(user_id, id);

-- +goose Down
DROP TABLE public.comparison_items;

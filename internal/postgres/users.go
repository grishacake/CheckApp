package postgres

import (
	"context"
	"errors"
	"fmt"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type userDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type UserRepository struct {
	db userDB
}

var _ auth.UserRepository = (*UserRepository)(nil)

// NewUserRepository accepts a pgx pool or transaction.
func NewUserRepository(db userDB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, email, nickname, passwordHash string) (model.User, error) {
	user, err := scanUser(r.db.QueryRow(ctx, `
		INSERT INTO public.users (email, nickname, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id::text, email, nickname, password_hash, created_at`,
		email, nickname, passwordHash))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			return model.User{}, auth.ErrEmailAlreadyExists
		}
		return model.User{}, fmt.Errorf("insert PostgreSQL user: %w", err)
	}
	return user, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (model.User, error) {
	user, err := scanUser(r.db.QueryRow(ctx, `
		SELECT id::text, email, nickname, password_hash, created_at
		FROM public.users WHERE email = $1`, email))
	return user, userReadError(err)
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	var userID pgtype.UUID
	if err := userID.Scan(id); err != nil {
		return model.User{}, auth.ErrUserNotFound
	}
	user, err := scanUser(r.db.QueryRow(ctx, `
		SELECT id::text, email, nickname, password_hash, created_at
		FROM public.users WHERE id = $1`, userID))
	return user, userReadError(err)
}

func scanUser(row pgx.Row) (model.User, error) {
	var user model.User
	if err := row.Scan(&user.ID, &user.Email, &user.Nickname, &user.PasswordHash, &user.CreatedAt); err != nil {
		return model.User{}, err
	}
	return user, nil
}

func userReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("read PostgreSQL user: %w", err)
	}
	return nil
}

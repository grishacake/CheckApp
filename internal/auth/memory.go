package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"app-grabber-backend/internal/model"
)

type MemoryUserRepository struct {
	mu     sync.RWMutex
	users  map[string]model.User
	emails map[string]string
}

func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{users: make(map[string]model.User), emails: make(map[string]string)}
}

func (r *MemoryUserRepository) Create(ctx context.Context, email, nickname, passwordHash string) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return model.User{}, fmt.Errorf("generate user ID: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	if _, exists := r.emails[email]; exists {
		return model.User{}, ErrEmailAlreadyExists
	}
	user := model.User{
		ID:    fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]),
		Email: email, Nickname: nickname, PasswordHash: passwordHash, CreatedAt: time.Now().UTC(),
	}
	r.users[user.ID] = user
	r.emails[email] = user.ID
	return user, nil
}

func (r *MemoryUserRepository) GetByEmail(ctx context.Context, email string) (model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	id, ok := r.emails[email]
	if !ok {
		return model.User{}, ErrUserNotFound
	}
	return r.users[id], nil
}

func (r *MemoryUserRepository) GetByID(ctx context.Context, id string) (model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	user, ok := r.users[id]
	if !ok {
		return model.User{}, ErrUserNotFound
	}
	return user, nil
}

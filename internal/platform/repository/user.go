package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dangyyyy/avito-kitchen/internal/platform/domain"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func scanUser(row pgx.CollectableRow) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Name, &u.Phone, &u.CreatedAt)

	return u, err
}

func (repo *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	rows, err := repo.pool.Query(ctx,
		`SELECT id, name, phone, created_at FROM users WHERE id = $1`, id)
	if err != nil {
		return domain.User{}, fmt.Errorf("query user: %w", err)
	}

	user, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, fmt.Errorf("user %s: %w", id, domain.ErrNotFound)
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}

	return user, nil
}

func (repo *UserRepository) List(ctx context.Context, limit, offset int) ([]domain.User, error) {
	rows, err := repo.pool.Query(ctx, `
		SELECT id, name, phone, created_at
		FROM users
		ORDER BY created_at
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}

	users, err := pgx.CollectRows(rows, scanUser)
	if err != nil {
		return nil, fmt.Errorf("scan users: %w", err)
	}

	return users, nil
}

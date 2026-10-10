package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wera/internal/store"
)

// userProfiles returns the profile of the user with the given email, or
// every ready profile when email is empty.
func userProfiles(ctx context.Context, pool *pgxpool.Pool, email string) ([]*store.Profile, error) {
	if email == "" {
		return store.ReadyProfiles(ctx, pool)
	}
	p, err := userProfile(ctx, pool, email)
	if err != nil {
		return nil, err
	}
	return []*store.Profile{p}, nil
}

// userProfile returns one user's profile, which must be ready to match.
func userProfile(ctx context.Context, pool *pgxpool.Pool, email string) (*store.Profile, error) {
	u, err := store.UserByEmail(ctx, pool, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no user with email %s", email)
	} else if err != nil {
		return nil, err
	}
	p, err := store.GetProfile(ctx, pool, u.ID)
	if err != nil {
		return nil, err
	}
	if p == nil || !p.Ready() {
		return nil, fmt.Errorf("%s has no complete profile yet (profile text, role families and levels)", email)
	}
	return p, nil
}

// onlyProfile returns the given user's profile, or the only ready profile
// when email is empty (an error when there are several).
func onlyProfile(ctx context.Context, pool *pgxpool.Pool, email string) (*store.Profile, error) {
	profiles, err := userProfiles(ctx, pool, email)
	if err != nil {
		return nil, err
	}
	switch len(profiles) {
	case 0:
		return nil, errors.New("no user has a complete profile yet")
	case 1:
		return profiles[0], nil
	}
	return nil, errors.New("several users have profiles; pass --user EMAIL")
}

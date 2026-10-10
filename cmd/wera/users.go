// Command dispatch for `wera users`: account administration from the
// server shell (create accounts, reset passwords, grant admin).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"wera/internal/auth"
	"wera/internal/config"
	"wera/internal/scoring"
	"wera/internal/store"
)

const usersUsage = `usage:
  wera users list
  wera users create --email E [--name N] [--admin]   prints a generated password
  wera users passwd --email E                       prints a new generated password
  wera users admin --email E [--revoke]
  wera users update --email E [--new-email X] [--name N]
  wera users import-profile --email E --file profile.md`

// runUsers implements `wera users ...`.
func runUsers(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return errors.New(usersUsage)
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("users "+sub, flag.ContinueOnError)
	email := fs.String("email", "", "account email")
	name := fs.String("name", "", "display name")
	admin := fs.Bool("admin", false, "grant admin")
	revoke := fs.Bool("revoke", false, "revoke admin instead of granting it")
	newEmail := fs.String("new-email", "", "update: the new email")
	file := fs.String("file", "", "import-profile: markdown file to use as the profile text")
	if err := fs.Parse(args); err != nil {
		return err
	}

	env, err := config.LoadEnv()
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if sub == "list" {
		rows, err := pool.Query(ctx, `
			SELECT id, email, name, is_admin, created_at::date::text, COALESCE(last_login_at::date::text, '-')
			FROM users ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		fmt.Printf("%-4s %-32s %-20s %-6s %-11s %s\n", "id", "email", "name", "admin", "created", "last login")
		for rows.Next() {
			var u store.User
			var created, last string
			if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &created, &last); err != nil {
				return err
			}
			fmt.Printf("%-4d %-32s %-20s %-6v %-11s %s\n", u.ID, u.Email, u.Name, u.IsAdmin, created, last)
		}
		return rows.Err()
	}

	norm, err := auth.NormalizeEmail(*email)
	if err != nil {
		return fmt.Errorf("--email: %w", err)
	}
	switch sub {
	case "create":
		pw, hash, err := newPassword()
		if err != nil {
			return err
		}
		u, err := store.CreateUser(ctx, pool, norm, *name, hash, *admin)
		if err != nil {
			return err
		}
		fmt.Printf("created user %d (%s, admin=%v)\npassword: %s\n", u.ID, u.Email, u.IsAdmin, pw)
	case "passwd":
		u, err := store.UserByEmail(ctx, pool, norm)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no user with email %s", norm)
		} else if err != nil {
			return err
		}
		pw, hash, err := newPassword()
		if err != nil {
			return err
		}
		if err := store.SetPassword(ctx, pool, u.ID, hash, ""); err != nil {
			return err
		}
		fmt.Printf("new password for %s (all their sessions were ended): %s\n", u.Email, pw)
	case "admin":
		tag, err := pool.Exec(ctx, `UPDATE users SET is_admin = $2 WHERE lower(email) = $1`, norm, !*revoke)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("no user with email %s", norm)
		}
		fmt.Printf("%s admin=%v\n", norm, !*revoke)
	case "update":
		u, err := store.UserByEmail(ctx, pool, norm)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no user with email %s", norm)
		} else if err != nil {
			return err
		}
		if *newEmail != "" {
			ne, err := auth.NormalizeEmail(*newEmail)
			if err != nil {
				return fmt.Errorf("--new-email: %w", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE users SET email = $2 WHERE id = $1`, u.ID, ne); err != nil {
				return err
			}
			u.Email = ne
		}
		if *name != "" {
			if _, err := pool.Exec(ctx, `UPDATE users SET name = $2 WHERE id = $1`, u.ID, *name); err != nil {
				return err
			}
		}
		fmt.Printf("updated user %d (%s)\n", u.ID, u.Email)
	case "import-profile":
		u, err := store.UserByEmail(ctx, pool, norm)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no user with email %s", norm)
		} else if err != nil {
			return err
		}
		md, err := os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("--file: %w", err)
		}
		prof, err := store.GetProfile(ctx, pool, u.ID)
		if err != nil {
			return err
		}
		var prefs config.Preferences
		if prof != nil {
			prefs = prof.Preferences
		}
		hash := scoring.ProfileHash(md)
		if err := store.SaveProfile(ctx, pool, u.ID, string(md), hash, prefs); err != nil {
			return err
		}
		fmt.Printf("profile for %s set from %s (hash %s)\n", u.Email, *file, hash[:12])
	default:
		return errors.New(usersUsage)
	}
	return nil
}

// newPassword generates a random password and its hash.
func newPassword() (pw, hash string, err error) {
	if pw, err = auth.RandomPassword(); err != nil {
		return "", "", err
	}
	hash, err = auth.HashPassword(pw)
	return pw, hash, err
}

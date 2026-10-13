package control

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidAppearance indicates an unsupported built-in visual theme.
var ErrInvalidAppearance = errors.New("invalid appearance configuration")

// Appearance contains the site's persisted built-in visual theme selection.
type Appearance struct {
	DefaultTheme string
}

// AppearanceConfig returns the singleton site appearance settings.
func (s *Store) AppearanceConfig(ctx context.Context) (Appearance, error) {
	var appearance Appearance
	if err := s.db.QueryRowContext(ctx, `SELECT default_theme FROM appearance WHERE singleton_id=1`).Scan(&appearance.DefaultTheme); err != nil {
		return Appearance{}, fmt.Errorf("read appearance: %w", err)
	}
	return appearance, nil
}

// UpdateAppearance saves a built-in theme without changing other settings.
func (s *Store) UpdateAppearance(ctx context.Context, appearance Appearance) error {
	if appearance.DefaultTheme != "emerald" && appearance.DefaultTheme != "classic" {
		return ErrInvalidAppearance
	}
	result, err := s.db.ExecContext(ctx, `UPDATE appearance SET default_theme=? WHERE singleton_id=1`, appearance.DefaultTheme)
	if err != nil {
		return fmt.Errorf("update appearance: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read appearance update result: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

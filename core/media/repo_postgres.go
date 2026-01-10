package media

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) Create(
	ctx context.Context,
	m *Media,
) error {
	_, err := r.db.Exec(
		ctx,
		`
    INSERT INTO media (
      id,
      user_id,
	  name,
      type,
      original_url,
      format,
      size_bytes,
      status,
      created_at
    ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
    `,
		m.ID,
		m.UserID,
		m.Name,
		m.Type,
		m.OriginalURL,
		m.Format,
		m.SizeBytes,
		m.Status,
		m.CreatedAt,
	)
	return err
}

func (r *PostgresRepository) ListByUser(ctx context.Context, userID string) ([]Media, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, name, type, original_url, format, size_bytes, status, created_at
		 FROM media
		 WHERE user_id=$1 AND type='image'
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []Media
	for rows.Next() {
		var img Media
		rows.Scan(
			&img.ID,
			&img.UserID,
			&img.Name,
			&img.Type,
			&img.OriginalURL,
			&img.Format,
			&img.SizeBytes,
			&img.Status,
			&img.CreatedAt,
		)
		images = append(images, img)
	}

	return images, nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id, userID string) (*Media, error) {
	var img Media

	err := r.db.QueryRow(ctx,
		`SELECT id, name, original_url, format, size_bytes, created_at
		 FROM media
		 WHERE id=$1 AND user_id=$2`,
		id,
		userID,
	).Scan(
		&img.ID,
		&img.Name,
		&img.OriginalURL,
		&img.Format,
		&img.SizeBytes,
		&img.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &img, nil
}

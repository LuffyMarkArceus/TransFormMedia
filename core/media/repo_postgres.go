package media

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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
			processed_url,
			thumbnail_url,
			format,
			size_bytes,
			width,
			height,
			duration_seconds,
			status,
			created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		`,
		m.ID,
		m.UserID,
		m.Name,
		m.Type,
		m.OriginalURL,
		m.ProcessedURL,
		m.ThumbnailURL,
		m.Format,
		m.SizeBytes,
		m.Width,
		m.Height,
		m.Duration,
		m.Status,
		m.CreatedAt,
	)
	return err
}

func (r *PostgresRepository) ListPaginated(ctx context.Context, params ListParams) (*PaginatedResult, error) {
	if params.Limit <= 0 || params.Limit > MaxLimit {
		params.Limit = DefaultLimit
	}
	if params.Offset < 0 {
		params.Offset = DefaultOffset
	}
	if params.SortBy == "" {
		params.SortBy = SortByCreatedAt
	}
	if params.SortDir == "" {
		params.SortDir = SortDirDesc
	}

	allowedSortBy := map[string]bool{
		SortByCreatedAt: true,
		SortByName:      true,
		SortBySize:      true,
	}
	if !allowedSortBy[params.SortBy] {
		params.SortBy = SortByCreatedAt
	}

	if params.SortDir != SortDirAsc && params.SortDir != SortDirDesc {
		params.SortDir = SortDirDesc
	}

	whereClause := "WHERE user_id=$1"
	args := []interface{}{params.UserID}
	argIdx := 2

	if params.Status != "" {
		whereClause += " AND status=$" + fmt.Sprintf("%d", argIdx)
		args = append(args, params.Status)
		argIdx++
	} else {
		whereClause += " AND status != $" + fmt.Sprintf("%d", argIdx)
		args = append(args, "trashed")
		argIdx++
	}

	if params.Type != "" {
		whereClause += " AND type=$" + fmt.Sprintf("%d", argIdx)
		args = append(args, params.Type)
		argIdx++
	}

	if params.Search != "" {
		whereClause += " AND name ILIKE $" + fmt.Sprintf("%d", argIdx)
		args = append(args, "%"+params.Search+"%")
		argIdx++
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM media " + whereClause
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	orderClause := fmt.Sprintf("ORDER BY %s %s", params.SortBy, params.SortDir)

	dataQuery := fmt.Sprintf(
		`SELECT id, user_id, name, type, original_url, processed_url, thumbnail_url, format, size_bytes, COALESCE(width,0), COALESCE(height,0), COALESCE(duration_seconds,0), status, created_at
		 FROM media %s %s LIMIT $%d OFFSET $%d`,
		whereClause, orderClause, argIdx, argIdx+1,
	)
	args = append(args, params.Limit, params.Offset)

	rows, err := r.db.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Name, &m.Type,
			&m.OriginalURL, &m.ProcessedURL, &m.ThumbnailURL,
			&m.Format, &m.SizeBytes, &m.Width, &m.Height,
			&m.Duration, &m.Status, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, m)
	}

	if items == nil {
		items = []Media{}
	}

	return &PaginatedResult{
		Data:  items,
		Total: total,
	}, nil
}

func (r *PostgresRepository) ListByUser(ctx context.Context, userID string) ([]Media, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, name, type, original_url, processed_url, thumbnail_url, format, size_bytes, COALESCE(width,0), COALESCE(height,0), COALESCE(duration_seconds,0), status, created_at
		 FROM media
		 WHERE user_id=$1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mediaItems []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(
			&m.ID,
			&m.UserID,
			&m.Name,
			&m.Type,
			&m.OriginalURL,
			&m.ProcessedURL,
			&m.ThumbnailURL,
			&m.Format,
			&m.SizeBytes,
			&m.Width,
			&m.Height,
			&m.Duration,
			&m.Status,
			&m.CreatedAt,
		); err != nil {
			return nil, err
		}
		mediaItems = append(mediaItems, m)
	}

	return mediaItems, nil
}

func (r *PostgresRepository) ListByUserAndType(ctx context.Context, userID string, mediaType string) ([]Media, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, name, type, original_url, processed_url, thumbnail_url, format, size_bytes, COALESCE(width,0), COALESCE(height,0), COALESCE(duration_seconds,0), status, created_at
		 FROM media
		 WHERE user_id=$1 AND type=$2
		 ORDER BY created_at DESC`,
		userID,
		mediaType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mediaItems []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(
			&m.ID,
			&m.UserID,
			&m.Name,
			&m.Type,
			&m.OriginalURL,
			&m.ProcessedURL,
			&m.ThumbnailURL,
			&m.Format,
			&m.SizeBytes,
			&m.Width,
			&m.Height,
			&m.Duration,
			&m.Status,
			&m.CreatedAt,
		); err != nil {
			return nil, err
		}
		mediaItems = append(mediaItems, m)
	}

	return mediaItems, nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id string) (*Media, error) {
	var m Media

	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, name, type, original_url, processed_url, thumbnail_url, format, size_bytes, COALESCE(width,0), COALESCE(height,0), COALESCE(duration_seconds,0), status, created_at
		 FROM media
		 WHERE id=$1`,
		id,
	).Scan(
		&m.ID,
		&m.UserID,
		&m.Name,
		&m.Type,
		&m.OriginalURL,
		&m.ProcessedURL,
		&m.ThumbnailURL,
		&m.Format,
		&m.SizeBytes,
		&m.Width,
		&m.Height,
		&m.Duration,
		&m.Status,
		&m.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &m, nil
}

func (r *PostgresRepository) GetByIDForUser(ctx context.Context, id, userID string) (*Media, error) {
	var m Media

	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, name, type, original_url, processed_url, thumbnail_url, format, size_bytes, COALESCE(width,0), COALESCE(height,0), COALESCE(duration_seconds,0), status, created_at
		 FROM media
		 WHERE id=$1 AND user_id=$2`,
		id,
		userID,
	).Scan(
		&m.ID,
		&m.UserID,
		&m.Name,
		&m.Type,
		&m.OriginalURL,
		&m.ProcessedURL,
		&m.ThumbnailURL,
		&m.Format,
		&m.SizeBytes,
		&m.Width,
		&m.Height,
		&m.Duration,
		&m.Status,
		&m.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &m, nil
}

func (r *PostgresRepository) DeleteByID(ctx context.Context, id string, userID string) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM media
		 WHERE id=$1 AND user_id=$2`,
		id,
		userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) UpdateName(ctx context.Context, id string, userID string, name string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE media
		 SET name = $1, updated_at = NOW()
		 WHERE id = $2 AND user_id = $3
		`,
		name,
		id,
		userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListByStatus(ctx context.Context, status string, limit int) ([]Media, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, name, type, original_url, processed_url, thumbnail_url, format, size_bytes, COALESCE(width,0), COALESCE(height,0), COALESCE(duration_seconds,0), status, created_at
		 FROM media
		 WHERE status=$1
		 ORDER BY created_at ASC
		 LIMIT $2`,
		status, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Name, &m.Type,
			&m.OriginalURL, &m.ProcessedURL, &m.ThumbnailURL,
			&m.Format, &m.SizeBytes, &m.Width, &m.Height,
			&m.Duration, &m.Status, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, m)
	}

	return items, nil
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, id string, userID string, status string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE media
		 SET status = $1, updated_at = NOW()
		 WHERE id = $2 AND user_id = $3
		`,
		status,
		id,
		userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) UpdateProcessedResult(
	ctx context.Context, id, userID, processedURL, thumbnailURL string,
	width, height, duration int,
) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE media
		 SET processed_url = $1, thumbnail_url = $2,
		     width = $3, height = $4, duration_seconds = $5,
		     status = 'ready', updated_at = NOW()
		 WHERE id = $6 AND user_id = $7
		`,
		processedURL, thumbnailURL, width, height, duration, id, userID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

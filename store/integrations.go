package store

import (
	"context"
	"time"
)

// Integration: một bot gắn vào một kênh.
type Integration struct {
	ID            int64     `json:"id"`
	ChannelID     int64     `json:"channel_id"`
	BotUserID     int64     `json:"bot_user_id"`
	BotUsername   string    `json:"bot_username"`
	BotName       string    `json:"bot_name"`
	OutgoingURL   string    `json:"outgoing_url"`
	SigningSecret string    `json:"signing_secret"`
	CreatedBy     *int64    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}

const integrationSelect = `
	SELECT i.id, i.channel_id, i.bot_user_id, u.username, u.display_name, i.outgoing_url, i.signing_secret,
		i.created_by, i.created_at
	FROM integrations i JOIN users u ON u.id = i.bot_user_id`

func scanIntegration(row scanner) (Integration, error) {
	var it Integration
	err := row.Scan(&it.ID, &it.ChannelID, &it.BotUserID, &it.BotUsername, &it.BotName, &it.OutgoingURL,
		&it.SigningSecret, &it.CreatedBy, &it.CreatedAt)
	return it, err
}

func (s *Store) CreateIntegration(ctx context.Context, channelID, botUserID, createdBy int64, tokenHash, signingSecret, outgoingURL string) (Integration, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO integrations (channel_id, bot_user_id, token_hash, signing_secret, outgoing_url, created_by)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		channelID, botUserID, tokenHash, signingSecret, outgoingURL, createdBy).Scan(&id)
	if err != nil {
		return Integration{}, err
	}
	return s.GetIntegration(ctx, id)
}

func (s *Store) GetIntegration(ctx context.Context, id int64) (Integration, error) {
	it, err := scanIntegration(s.db.QueryRowContext(ctx, integrationSelect+` WHERE i.id = ?`, id))
	return it, notFound(err)
}

// IntegrationByToken: bot ứng với mã bí mật của link webhook (đã băm).
func (s *Store) IntegrationByToken(ctx context.Context, tokenHash string) (Integration, error) {
	it, err := scanIntegration(s.db.QueryRowContext(ctx, integrationSelect+` WHERE i.token_hash = ?`, tokenHash))
	return it, notFound(err)
}

func (s *Store) ListIntegrations(ctx context.Context, channelID int64) ([]Integration, error) {
	rows, err := s.db.QueryContext(ctx, integrationSelect+` WHERE i.channel_id = ? ORDER BY i.id`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Integration{}
	for rows.Next() {
		it, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) UpdateIntegrationURL(ctx context.Context, id int64, outgoingURL string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE integrations SET outgoing_url = ? WHERE id = ?`, outgoingURL, id)
	return err
}

func (s *Store) RotateIntegrationToken(ctx context.Context, id int64, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE integrations SET token_hash = ? WHERE id = ?`, tokenHash, id)
	return err
}

// DeleteIntegration xoá bot khỏi kênh. Tài khoản bot giữ lại để các tin cũ vẫn có tên người gửi.
func (s *Store) DeleteIntegration(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM integrations WHERE id = ?`, id)
	return err
}

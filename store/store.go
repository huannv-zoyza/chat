// Package store chứa toàn bộ truy vấn SQL. Handler không viết SQL trực tiếp.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"chat/model"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

/* ---------- Users ---------- */

const userColumns = `id, username, email, password_hash, display_name, avatar, status_text, status, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.DisplayName,
		&u.Avatar, &u.StatusText, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (s *Store) CreateUser(ctx context.Context, username, email, passwordHash, displayName string) (model.User, error) {
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO users (username, email, password_hash, display_name)
		VALUES (?, ?, ?, ?)
		RETURNING `+userColumns, username, email, passwordHash, displayName)
	u, err := scanUser(row)
	if isUniqueViolation(err) {
		return u, ErrConflict
	}
	return u, err
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (model.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	return u, notFound(err)
}

// GetUserByLogin tìm theo username hoặc email (không phân biệt hoa thường).
func (s *Store) GetUserByLogin(ctx context.Context, login string) (model.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = lower(?) OR lower(email) = lower(?)`, login, login))
	return u, notFound(err)
}

func (s *Store) UpdateProfile(ctx context.Context, id int64, displayName string, avatar *string, statusText string) (model.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `
		UPDATE users
		SET display_name = ?, avatar = ?, status_text = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		RETURNING `+userColumns, displayName, avatar, statusText, id))
	return u, notFound(err)
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, passwordHash, id)
	return err
}

/* ---------- Sessions ---------- */

func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, tokenHash, userID, expiresAt.UTC())
	return err
}

// UserBySession trả về người dùng của phiên còn hạn; phiên hết hạn bị xoá luôn.
func (s *Store) UserBySession(ctx context.Context, tokenHash string) (model.User, error) {
	var expiresAt time.Time
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`, tokenHash).Scan(&userID, &expiresAt)
	if err != nil {
		return model.User{}, notFound(err)
	}
	if time.Now().After(expiresAt) {
		_ = s.DeleteSession(ctx, tokenHash)
		return model.User{}, ErrNotFound
	}
	return s.GetUserByID(ctx, userID)
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteOtherSessions đăng xuất mọi thiết bị khác (dùng khi đổi mật khẩu).
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keepTokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepTokenHash)
	return err
}

/* ---------- Channels ---------- */

const channelColumns = `c.id, c.name, c.description, c.owner_id, c.is_private, c.kind, COALESCE(c.invite_code, ''), c.created_at, c.updated_at,
	(SELECT COUNT(*) FROM channel_members m WHERE m.channel_id = c.id)`

func scanChannel(row scanner, extra ...any) (model.Channel, error) {
	var c model.Channel
	dest := append([]any{&c.ID, &c.Name, &c.Description, &c.OwnerID, &c.IsPrivate, &c.Kind, &c.InviteCode,
		&c.CreatedAt, &c.UpdatedAt, &c.MemberCount}, extra...)
	err := row.Scan(dest...)
	return c, err
}

// ListMyChannels: các kênh userID đã tham gia, kèm số tin chưa đọc (không tính tin của chính mình).
func (s *Store) ListMyChannels(ctx context.Context, userID int64) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+channelColumns+`,
			(SELECT COUNT(*) FROM messages msg
			 WHERE msg.channel_id = c.id AND msg.id > cm.last_read_message_id AND msg.user_id <> cm.user_id
			   AND msg.parent_id IS NULL AND msg.deleted_at IS NULL),
			(SELECT COUNT(*) FROM message_mentions mm JOIN messages msg ON msg.id = mm.message_id
			 WHERE mm.user_id = cm.user_id AND msg.channel_id = c.id AND msg.id > cm.last_read_message_id)
		FROM channels c
		JOIN channel_members cm ON cm.channel_id = c.id AND cm.user_id = ?
		ORDER BY c.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Channel{}
	for rows.Next() {
		var unread, mentions int
		c, err := scanChannel(rows, &unread, &mentions)
		if err != nil {
			return nil, err
		}
		c.UnreadCount = unread
		c.MentionCount = mentions
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.attachDMMembers(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachDMMembers điền hai người trong cuộc của các cuộc trò chuyện riêng.
func (s *Store) attachDMMembers(ctx context.Context, chans []model.Channel) error {
	for i := range chans {
		if !chans[i].IsDM() {
			continue
		}
		members, err := s.ListMembers(ctx, chans[i].ID)
		if err != nil {
			return err
		}
		chans[i].DMMembers = members
	}
	return nil
}

// DMName: tên nội bộ của cuộc trò chuyện riêng giữa a và b (không phụ thuộc thứ tự).
func DMName(a, b int64) string {
	if a > b {
		a, b = b, a
	}
	return fmt.Sprintf("dm:%d:%d", a, b)
}

// OpenDM trả về cuộc trò chuyện riêng giữa a và b, tạo mới nếu chưa có (created = true).
func (s *Store) OpenDM(ctx context.Context, a, b int64) (model.Channel, bool, error) {
	name := DMName(a, b)
	find := func() (model.Channel, error) {
		c, err := scanChannel(s.db.QueryRowContext(ctx,
			`SELECT `+channelColumns+` FROM channels c WHERE c.name = ? AND c.kind = 'dm'`, name))
		if err != nil {
			return c, notFound(err)
		}
		chans := []model.Channel{c}
		if err := s.attachDMMembers(ctx, chans); err != nil {
			return c, err
		}
		return chans[0], nil
	}
	if c, err := find(); err == nil {
		return c, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return c, false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Channel{}, false, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO channels (name, description, owner_id, is_private, kind) VALUES (?, '', ?, 1, 'dm') RETURNING id`,
		name, a).Scan(&id)
	if isUniqueViolation(err) {
		// người kia vừa mở cùng lúc
		c, err := find()
		return c, false, err
	}
	if err != nil {
		return model.Channel{}, false, err
	}
	for _, uid := range []int64{a, b} {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO channel_members (channel_id, user_id) VALUES (?, ?)`, id, uid); err != nil {
			return model.Channel{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Channel{}, false, err
	}
	c, err := find()
	return c, true, err
}

// SearchUsers tìm người dùng theo username/tên hiển thị (q rỗng: lấy theo thứ tự tên).
func (s *Store) SearchUsers(ctx context.Context, q string, limit int) ([]model.PublicUser, error) {
	like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+userColumns+` FROM users
		WHERE lower(username) LIKE ? ESCAPE '\' OR lower(display_name) LIKE ? ESCAPE '\'
		ORDER BY CASE WHEN display_name = '' THEN username ELSE display_name END
		LIMIT ?`, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PublicUser{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u.Public())
	}
	return out, rows.Err()
}

// ListOtherChannels: các kênh CÔNG KHAI userID chưa tham gia, để khám phá.
// Kênh riêng tư không bao giờ xuất hiện ở đây.
func (s *Store) ListOtherChannels(ctx context.Context, userID int64) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+channelColumns+`
		FROM channels c
		WHERE c.is_private = 0
		  AND NOT EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = c.id AND cm.user_id = ?)
		ORDER BY c.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, id int64) (model.Channel, error) {
	c, err := scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelColumns+` FROM channels c WHERE c.id = ?`, id))
	if err != nil {
		return c, notFound(err)
	}
	chans := []model.Channel{c}
	if err := s.attachDMMembers(ctx, chans); err != nil {
		return c, err
	}
	return chans[0], nil
}

// CreateChannel tạo kênh và thêm người tạo làm thành viên trong cùng transaction.
// newInviteCodeSQL sinh mã mời 24 ký tự hex ngẫu nhiên ngay trong SQLite.
const newInviteCodeSQL = `lower(hex(randomblob(12)))`

func (s *Store) CreateChannel(ctx context.Context, ownerID int64, name, description string, isPrivate bool) (model.Channel, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Channel{}, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO channels (name, description, owner_id, is_private, invite_code)
		 VALUES (?, ?, ?, ?, `+newInviteCodeSQL+`) RETURNING id`,
		name, description, ownerID, isPrivate).Scan(&id)
	if isUniqueViolation(err) {
		return model.Channel{}, ErrConflict
	}
	if err != nil {
		return model.Channel{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO channel_members (channel_id, user_id) VALUES (?, ?)`, id, ownerID); err != nil {
		return model.Channel{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Channel{}, err
	}
	return s.GetChannel(ctx, id)
}

func (s *Store) UpdateChannel(ctx context.Context, id int64, name, description string, isPrivate bool) (model.Channel, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE channels SET name = ?, description = ?, is_private = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		name, description, isPrivate, id)
	if isUniqueViolation(err) {
		return model.Channel{}, ErrConflict
	}
	if err != nil {
		return model.Channel{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.Channel{}, ErrNotFound
	}
	return s.GetChannel(ctx, id)
}

// ChannelByInvite tìm kênh theo mã trong link mời.
func (s *Store) ChannelByInvite(ctx context.Context, code string) (model.Channel, error) {
	c, err := scanChannel(s.db.QueryRowContext(ctx,
		`SELECT `+channelColumns+` FROM channels c WHERE c.invite_code = ? AND c.kind = 'channel'`, code))
	return c, notFound(err)
}

// RegenerateInvite tạo mã mời mới; link cũ hết hiệu lực ngay.
func (s *Store) RegenerateInvite(ctx context.Context, id int64) (model.Channel, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE channels SET invite_code = `+newInviteCodeSQL+`, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id); err != nil {
		return model.Channel{}, err
	}
	return s.GetChannel(ctx, id)
}

// DeleteChannel: thành viên và tin nhắn bị xoá theo ON DELETE CASCADE.
func (s *Store) DeleteChannel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM channels WHERE id = ?`, id)
	return err
}

// AddMember: người mới tham gia coi như đã đọc hết tin cũ, nên không bị báo hàng trăm tin chưa đọc.
// Trả về false nếu đã là thành viên từ trước.
func (s *Store) AddMember(ctx context.Context, channelID, userID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO channel_members (channel_id, user_id, last_read_message_id)
		VALUES (?, ?, COALESCE((SELECT MAX(id) FROM messages WHERE channel_id = ?), 0))`,
		channelID, userID, channelID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) RemoveMember(ctx context.Context, channelID, userID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM channel_members WHERE channel_id = ? AND user_id = ?`, channelID, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) IsMember(ctx context.Context, channelID, userID int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM channel_members WHERE channel_id = ? AND user_id = ?`, channelID, userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) ListMembers(ctx context.Context, channelID int64) ([]model.PublicUser, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+userColumns+` FROM users
		WHERE id IN (SELECT user_id FROM channel_members WHERE channel_id = ?)
		ORDER BY CASE WHEN display_name = '' THEN username ELSE display_name END`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.PublicUser{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u.Public())
	}
	return out, rows.Err()
}

func (s *Store) ChannelIDsForUser(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id FROM channel_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkRead chỉ tiến lên, không bao giờ lùi lại (tránh thứ tự gói tin đến lệch nhau), và không vượt
// quá tin mới nhất của kênh. Trả về vị trí mới và true nếu vị trí đọc thực sự thay đổi.
func (s *Store) MarkRead(ctx context.Context, channelID, userID, messageID int64) (int64, bool, error) {
	var pos int64
	err := s.db.QueryRowContext(ctx, `
		UPDATE channel_members
		   SET last_read_message_id = MIN(?, (SELECT COALESCE(MAX(id), 0) FROM messages WHERE channel_id = ?))
		 WHERE channel_id = ? AND user_id = ?
		   AND last_read_message_id < MIN(?, (SELECT COALESCE(MAX(id), 0) FROM messages WHERE channel_id = ?))
		RETURNING last_read_message_id`,
		messageID, channelID, channelID, userID, messageID, channelID).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return pos, true, nil
}

// ReadStates: mỗi thành viên của kênh đã đọc tới tin nào (để hiện "đã xem").
func (s *Store) ReadStates(ctx context.Context, channelID int64) ([]model.ReadState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cm.last_read_message_id, u.id, u.username, u.display_name, u.avatar, u.status_text
		FROM channel_members cm JOIN users u ON u.id = cm.user_id
		WHERE cm.channel_id = ?`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ReadState{}
	for rows.Next() {
		var r model.ReadState
		var u model.PublicUser
		if err := rows.Scan(&r.MessageID, &u.ID, &u.Username, &u.DisplayName, &u.Avatar, &u.StatusText); err != nil {
			return nil, err
		}
		r.UserID, r.User = u.ID, &u
		out = append(out, r)
	}
	return out, rows.Err()
}

/* ---------- Messages ---------- */

const messageColumns = `msg.id, msg.channel_id, msg.user_id, msg.content, msg.created_at, msg.edited_at, msg.deleted_at, msg.embed,
	msg.parent_id, COALESCE(p.user_id, 0),
	u.id, u.username, u.display_name, u.avatar, u.status_text`

// messageFrom đi kèm messageColumns: người gửi + tin gốc (nếu là trả lời trong luồng).
const messageFrom = ` FROM messages msg
	JOIN users u ON u.id = msg.user_id
	LEFT JOIN messages p ON p.id = msg.parent_id `

func scanMessage(row scanner) (model.Message, error) {
	var m model.Message
	var u model.PublicUser
	var embed sql.NullString
	err := row.Scan(&m.ID, &m.ChannelID, &m.UserID, &m.Content, &m.CreatedAt, &m.EditedAt, &m.DeletedAt, &embed,
		&m.ParentID, &m.ParentUserID,
		&u.ID, &u.Username, &u.DisplayName, &u.Avatar, &u.StatusText)
	m.User = &u
	if embed.Valid && embed.String != "" {
		var e model.Embed
		if json.Unmarshal([]byte(embed.String), &e) == nil {
			m.Embed = &e
		}
	}
	m.Attachments = []model.Attachment{}
	m.Mentions = []int64{}
	m.Reactions = []model.Reaction{}
	return m, err
}

// GetMessage lấy một tin (kèm file, mention, reaction, tóm tắt luồng).
func (s *Store) GetMessage(ctx context.Context, id int64) (model.Message, error) {
	m, err := scanMessage(s.db.QueryRowContext(ctx, `SELECT `+messageColumns+messageFrom+`WHERE msg.id = ?`, id))
	if err != nil {
		return model.Message{}, notFound(err)
	}
	msgs := []model.Message{m}
	if err := s.loadExtras(ctx, msgs); err != nil {
		return model.Message{}, err
	}
	return msgs[0], nil
}

// AttachmentURL là đường dẫn tải file, phục vụ bởi GET /api/files/{key}.
func AttachmentURL(key string) string {
	return "/api/files/" + key
}

// NewMessage là dữ liệu để tạo một tin nhắn.
type NewMessage struct {
	ChannelID   int64
	UserID      int64
	Content     string
	Attachments []model.Attachment // StorageKey, FileName, ContentType, Size, Width, Height
	MentionIDs  []int64
	ParentID    *int64 // khác nil: trả lời trong luồng của tin này
	Embed       *model.Embed
}

// CreateMessage lưu tin nhắn, file đính kèm và người được nhắc trong cùng một transaction.
func (s *Store) CreateMessage(ctx context.Context, in NewMessage) (model.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Message{}, err
	}
	defer tx.Rollback()

	var embed sql.NullString
	if in.Embed != nil {
		b, err := json.Marshal(in.Embed)
		if err != nil {
			return model.Message{}, err
		}
		embed = sql.NullString{String: string(b), Valid: true}
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO messages (channel_id, user_id, content, parent_id, embed) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		in.ChannelID, in.UserID, in.Content, in.ParentID, embed).Scan(&id); err != nil {
		return model.Message{}, err
	}
	for _, a := range in.Attachments {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO attachments (message_id, storage_key, file_name, content_type, size, width, height)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, a.StorageKey, a.FileName, a.ContentType, a.Size, a.Width, a.Height); err != nil {
			return model.Message{}, err
		}
	}
	for _, uid := range in.MentionIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO message_mentions (message_id, user_id) VALUES (?, ?)`, id, uid); err != nil {
			return model.Message{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Message{}, err
	}
	return s.GetMessage(ctx, id)
}

// EditMessage đổi nội dung tin (và danh sách người được nhắc), ghi lại thời điểm sửa.
func (s *Store) EditMessage(ctx context.Context, id int64, content string, mentionIDs []int64) (model.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Message{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE messages SET content = ?, edited_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`, content, id)
	if err != nil {
		return model.Message{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.Message{}, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_mentions WHERE message_id = ?`, id); err != nil {
		return model.Message{}, err
	}
	for _, uid := range mentionIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO message_mentions (message_id, user_id) VALUES (?, ?)`, id, uid); err != nil {
			return model.Message{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Message{}, err
	}
	return s.GetMessage(ctx, id)
}

// DeleteMessage xoá mềm: giữ một dòng "tin đã bị xoá" (luồng trả lời vẫn còn gốc), xoá sạch nội dung,
// file, cảm xúc, nhắc tên. Trả về storage key các file để xoá khỏi đĩa.
func (s *Store) DeleteMessage(ctx context.Context, id int64) (model.Message, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Message{}, nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT storage_key FROM attachments WHERE message_id = ?`, id)
	if err != nil {
		return model.Message{}, nil, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return model.Message{}, nil, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	for _, q := range []string{
		`DELETE FROM attachments WHERE message_id = ?`,
		`DELETE FROM message_mentions WHERE message_id = ?`,
		`DELETE FROM message_reactions WHERE message_id = ?`,
		`UPDATE messages SET content = '', embed = NULL, deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return model.Message{}, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Message{}, nil, err
	}
	m, err := s.GetMessage(ctx, id)
	return m, keys, err
}

// ListMessages trả về tối đa limit tin ở DÒNG CHÍNH của kênh (không gồm trả lời trong luồng)
// có id < beforeID (0 = mới nhất), theo thứ tự cũ -> mới, và hasMore cho biết còn tin cũ hơn không.
func (s *Store) ListMessages(ctx context.Context, channelID, beforeID int64, limit int) ([]model.Message, bool, error) {
	if beforeID <= 0 {
		beforeID = 1<<63 - 1
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+messageColumns+messageFrom+`
		WHERE msg.channel_id = ? AND msg.id < ? AND msg.parent_id IS NULL
		ORDER BY msg.id DESC
		LIMIT ?`, channelID, beforeID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := []model.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	rows.Close()

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	// đảo lại thành cũ -> mới
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if err := s.loadExtras(ctx, out); err != nil {
		return nil, false, err
	}
	return out, hasMore, nil
}

// loadExtras nạp file đính kèm và người được nhắc cho cả lô tin nhắn bằng 2 truy vấn.
func (s *Store) loadExtras(ctx context.Context, msgs []model.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	idx := make(map[int64]int, len(msgs))
	args := make([]any, len(msgs))
	for i, m := range msgs {
		idx[m.ID] = i
		args[i] = m.ID
	}
	in := "(?" + strings.Repeat(",?", len(msgs)-1) + ")"

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, message_id, storage_key, file_name, content_type, size, width, height, created_at
		FROM attachments WHERE message_id IN `+in+` ORDER BY id`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a model.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.StorageKey, &a.FileName, &a.ContentType,
			&a.Size, &a.Width, &a.Height, &a.CreatedAt); err != nil {
			rows.Close()
			return err
		}
		a.URL = AttachmentURL(a.StorageKey)
		m := &msgs[idx[a.MessageID]]
		m.Attachments = append(m.Attachments, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = s.db.QueryContext(ctx,
		`SELECT message_id, user_id FROM message_mentions WHERE message_id IN `+in, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mid, uid int64
		if err := rows.Scan(&mid, &uid); err != nil {
			rows.Close()
			return err
		}
		m := &msgs[idx[mid]]
		m.Mentions = append(m.Mentions, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	reactions, err := s.reactionsFor(ctx, in, args)
	if err != nil {
		return err
	}
	for mid, list := range reactions {
		msgs[idx[mid]].Reactions = list
	}

	return s.loadThreadSummaries(ctx, msgs, idx, in, args)
}

// reactionsFor gom reaction của nhiều tin: message_id -> [{emoji, user_ids}].
func (s *Store) reactionsFor(ctx context.Context, in string, args []any) (map[int64][]model.Reaction, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT message_id, emoji, user_id FROM message_reactions
		WHERE message_id IN `+in+` ORDER BY created_at, rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]model.Reaction{}
	for rows.Next() {
		var mid, uid int64
		var emoji string
		if err := rows.Scan(&mid, &emoji, &uid); err != nil {
			return nil, err
		}
		list := out[mid]
		i := 0
		for i < len(list) && list[i].Emoji != emoji {
			i++
		}
		if i == len(list) {
			list = append(list, model.Reaction{Emoji: emoji})
		}
		list[i].UserIDs = append(list[i].UserIDs, uid)
		out[mid] = list
	}
	return out, rows.Err()
}

// loadThreadSummaries: số trả lời, giờ trả lời gần nhất, tối đa 3 người trả lời gần nhất.
func (s *Store) loadThreadSummaries(ctx context.Context, msgs []model.Message, idx map[int64]int, in string, args []any) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT parent_id, COUNT(*), MAX(id) FROM messages
		WHERE parent_id IN `+in+` GROUP BY parent_id`, args...)
	if err != nil {
		return err
	}
	lastIDs := map[int64]int64{} // id trả lời mới nhất -> id tin gốc
	for rows.Next() {
		var pid, lastID int64
		var count int
		if err := rows.Scan(&pid, &count, &lastID); err != nil {
			rows.Close()
			return err
		}
		msgs[idx[pid]].Thread = &model.ThreadSummary{ReplyCount: count, ParticipantIDs: []int64{}}
		lastIDs[lastID] = pid
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(lastIDs) == 0 {
		return err
	}

	// giờ của trả lời mới nhất (đọc từ cột created_at để có kiểu TIMESTAMP)
	lastArgs := make([]any, 0, len(lastIDs))
	for id := range lastIDs {
		lastArgs = append(lastArgs, id)
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id, created_at FROM messages WHERE id IN (?`+
		strings.Repeat(",?", len(lastArgs)-1)+`)`, lastArgs...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return err
		}
		msgs[idx[lastIDs[id]]].Thread.LastReplyAt = at
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// người tham gia, ai trả lời gần nhất đứng trước
	rows, err = s.db.QueryContext(ctx, `
		SELECT parent_id, user_id, MAX(id) AS last FROM messages
		WHERE parent_id IN `+in+` GROUP BY parent_id, user_id ORDER BY last DESC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var pid, uid, last int64
		if err := rows.Scan(&pid, &uid, &last); err != nil {
			return err
		}
		t := msgs[idx[pid]].Thread
		if len(t.ParticipantIDs) < 3 {
			t.ParticipantIDs = append(t.ParticipantIDs, uid)
		}
	}
	return rows.Err()
}

// ListThread: các trả lời của tin gốc parentID, cũ -> mới (tối đa 1000).
func (s *Store) ListThread(ctx context.Context, parentID int64) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+messageColumns+messageFrom+`
		WHERE msg.parent_id = ? ORDER BY msg.id LIMIT 1000`, parentID)
	if err != nil {
		return nil, err
	}
	out := []model.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.loadExtras(ctx, out)
}

/* ---------- Reactions ---------- */

// MaxReactionKinds: số loại emoji khác nhau tối đa trên một tin.
const MaxReactionKinds = 20

var ErrTooManyReactions = errors.New("too many reaction kinds")

// ToggleReaction: chưa thả thì thả, đã thả thì bỏ. Trả về danh sách reaction mới của tin.
func (s *Store) ToggleReaction(ctx context.Context, messageID, userID int64, emoji string) ([]model.Reaction, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM message_reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`, messageID, userID, emoji)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var kinds int
		var exists bool
		if err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT emoji), COALESCE(MAX(emoji = ?), 0) FROM message_reactions WHERE message_id = ?`,
			emoji, messageID).Scan(&kinds, &exists); err != nil {
			return nil, err
		}
		if !exists && kinds >= MaxReactionKinds {
			return nil, ErrTooManyReactions
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO message_reactions (message_id, user_id, emoji) VALUES (?, ?, ?)`,
			messageID, userID, emoji); err != nil {
			return nil, err
		}
	}
	list, err := s.reactionsFor(ctx, "(?)", []any{messageID})
	if err != nil {
		return nil, err
	}
	if list[messageID] == nil {
		return []model.Reaction{}, nil
	}
	return list[messageID], nil
}

// ResolveMentions đổi các username được nhắc thành id thành viên của kênh.
// all = true (có @all) thì lấy mọi thành viên. Không bao gồm người gửi.
func (s *Store) ResolveMentions(ctx context.Context, channelID, senderID int64, usernames []string, all bool) ([]int64, error) {
	var rows *sql.Rows
	var err error
	if all {
		rows, err = s.db.QueryContext(ctx,
			`SELECT user_id FROM channel_members WHERE channel_id = ? AND user_id <> ?`, channelID, senderID)
	} else {
		if len(usernames) == 0 {
			return nil, nil
		}
		args := []any{channelID, senderID}
		for _, u := range usernames {
			args = append(args, u)
		}
		rows, err = s.db.QueryContext(ctx, `
			SELECT cm.user_id FROM channel_members cm JOIN users u ON u.id = cm.user_id
			WHERE cm.channel_id = ? AND cm.user_id <> ?
			  AND u.username IN (?`+strings.Repeat(",?", len(usernames)-1)+`)`, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AttachmentByKey tìm file theo storage_key.
func (s *Store) AttachmentByKey(ctx context.Context, key string) (model.Attachment, error) {
	var a model.Attachment
	err := s.db.QueryRowContext(ctx, `
		SELECT id, message_id, storage_key, file_name, content_type, size, width, height, created_at
		FROM attachments WHERE storage_key = ?`, key).
		Scan(&a.ID, &a.MessageID, &a.StorageKey, &a.FileName, &a.ContentType, &a.Size, &a.Width, &a.Height, &a.CreatedAt)
	a.URL = AttachmentURL(a.StorageKey)
	return a, notFound(err)
}

// AttachmentKeysForChannel: mọi file của kênh, để xoá khỏi đĩa khi xoá kênh.
func (s *Store) AttachmentKeysForChannel(ctx context.Context, channelID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.storage_key FROM attachments a JOIN messages m ON m.id = a.message_id
		WHERE m.channel_id = ?`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

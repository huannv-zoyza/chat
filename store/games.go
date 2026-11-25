package store

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"chat/model"
)

// GameRow: một ván đã lưu (state là JSON của package game).
type GameRow struct {
	MessageID int64
	ChannelID int64
	Kind      string
	State     []byte
}

// CreateGame tạo tin nhắn chứa ván chơi (embed type "game") cùng trạng thái ban đầu.
func (s *Store) CreateGame(ctx context.Context, channelID, userID int64, embed model.Embed, status string, state []byte) (model.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Message{}, err
	}
	defer tx.Rollback()
	e, err := json.Marshal(embed)
	if err != nil {
		return model.Message{}, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO messages (channel_id, user_id, content, embed) VALUES (?, ?, '', ?) RETURNING id`,
		channelID, userID, string(e)).Scan(&id); err != nil {
		return model.Message{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO games (message_id, kind, status, state) VALUES (?, ?, ?, ?)`,
		id, embed.ID, status, string(state)); err != nil {
		return model.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Message{}, err
	}
	return s.GetMessage(ctx, id)
}

// SaveGame ghi trạng thái mới của ván.
func (s *Store) SaveGame(ctx context.Context, messageID int64, status string, state []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE games SET status = ?, state = ?, updated_at = CURRENT_TIMESTAMP WHERE message_id = ?`,
		status, string(state), messageID)
	return err
}

// GetGame đọc ván gắn với tin messageID (ErrNotFound nếu không có hoặc tin đã xoá).
func (s *Store) GetGame(ctx context.Context, messageID int64) (GameRow, error) {
	g := GameRow{MessageID: messageID}
	var state string
	err := s.db.QueryRowContext(ctx, `
		SELECT m.channel_id, g.kind, g.state FROM games g JOIN messages m ON m.id = g.message_id
		WHERE g.message_id = ? AND m.deleted_at IS NULL`, messageID).Scan(&g.ChannelID, &g.Kind, &state)
	g.State = []byte(state)
	return g, notFound(err)
}

// ActiveGames: các ván chưa kết thúc (nạp lại khi server khởi động để chạy tiếp đồng hồ).
func (s *Store) ActiveGames(ctx context.Context) ([]GameRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.message_id, m.channel_id, g.kind, g.state FROM games g JOIN messages m ON m.id = g.message_id
		WHERE g.status <> 'ended' AND m.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GameRow
	for rows.Next() {
		var g GameRow
		var state string
		if err := rows.Scan(&g.MessageID, &g.ChannelID, &g.Kind, &state); err != nil {
			return nil, err
		}
		g.State = []byte(state)
		out = append(out, g)
	}
	return out, rows.Err()
}

// ActiveGamesByChannel trả về danh sách các ván game chưa kết thúc trong một kênh cụ thể.
func (s *Store) ActiveGamesByChannel(ctx context.Context, channelID int64) ([]GameRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.message_id, m.channel_id, g.kind, g.state 
		FROM games g 
		JOIN messages m ON m.id = g.message_id
		WHERE m.channel_id = ? 
		  AND g.status IN ('lobby', 'playing') 
		  AND m.deleted_at IS NULL
		ORDER BY m.id DESC`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GameRow
	for rows.Next() {
		var g GameRow
		var state string
		if err := rows.Scan(&g.MessageID, &g.ChannelID, &g.Kind, &state); err != nil {
			return nil, err
		}
		g.State = []byte(state)
		out = append(out, g)
	}
	return out, rows.Err()
}

type MemberGameStats struct {
	UserID        int64   `json:"user_id"`
	Username      string  `json:"username"`
	DisplayName   string  `json:"display_name"`
	Avatar        *string `json:"avatar"`
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	Draws         int     `json:"draws"`
	TotalGames    int     `json:"total_games"`
	WinRate       float64 `json:"win_rate"`
	NetProfit     int64   `json:"net_profit"`
	WalletBalance int64   `json:"wallet_balance"`
}

type WalletLeader struct {
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Avatar      *string `json:"avatar"`
	Balance     int64   `json:"balance"`
	NetProfit   int64   `json:"net_profit"`
}

type LeaderboardResponse struct {
	Kind       string            `json:"kind"`
	TopWins    []MemberGameStats `json:"top_wins"`
	TopLosses  []MemberGameStats `json:"top_losses"`
	TopRichest []WalletLeader    `json:"top_richest"`
	Members    []MemberGameStats `json:"members"`
	TotalEnded int               `json:"total_ended"`
}

type gameSummaryState struct {
	Players []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"players"`
	Winners []int64 `json:"winners"`
}

// GetGameLeaderboard tổng hợp bảng xếp hạng thành viên dựa trên các ván chơi đã kết thúc.
func (s *Store) GetGameLeaderboard(ctx context.Context, kind string) (LeaderboardResponse, error) {
	query := `
		SELECT g.kind, g.state
		FROM games g
		JOIN messages m ON m.id = g.message_id
		WHERE g.status = 'ended' AND m.deleted_at IS NULL`
	var args []any
	if kind != "" && kind != "all" {
		query += " AND g.kind = ?"
		args = append(args, kind)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return LeaderboardResponse{}, err
	}
	defer rows.Close()

	type rawStats struct {
		id       int64
		fallback string
		wins     int
		losses   int
		draws    int
		total    int
	}

	statsMap := make(map[int64]*rawStats)
	var totalEnded int

	for rows.Next() {
		var gKind string
		var stateStr string
		if err := rows.Scan(&gKind, &stateStr); err != nil {
			return LeaderboardResponse{}, err
		}
		totalEnded++

		var sum gameSummaryState
		if err := json.Unmarshal([]byte(stateStr), &sum); err != nil {
			continue
		}

		if len(sum.Players) == 0 {
			continue
		}

		for _, p := range sum.Players {
			if _, ok := statsMap[p.ID]; !ok {
				statsMap[p.ID] = &rawStats{id: p.ID, fallback: p.Name}
			}
		}

		if len(sum.Winners) == 0 {
			for _, p := range sum.Players {
				st := statsMap[p.ID]
				st.draws++
				st.total++
			}
		} else {
			winSet := make(map[int64]bool, len(sum.Winners))
			for _, wid := range sum.Winners {
				winSet[wid] = true
				if st, ok := statsMap[wid]; ok {
					st.wins++
					st.total++
				}
			}
			for _, p := range sum.Players {
				if !winSet[p.ID] {
					st := statsMap[p.ID]
					st.losses++
					st.total++
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return LeaderboardResponse{}, err
	}

	profitMap := make(map[int64]int64)
	profitQuery := `
		SELECT gb.user_id, COALESCE(SUM(gb.payout - gb.amount), 0)
		FROM game_bets gb
		JOIN games g ON g.message_id = gb.message_id
		JOIN messages m ON m.id = g.message_id
		WHERE gb.status IN ('won', 'lost') AND m.deleted_at IS NULL`
	var profitArgs []any
	if kind != "" && kind != "all" {
		profitQuery += " AND g.kind = ?"
		profitArgs = append(profitArgs, kind)
	}
	profitQuery += " GROUP BY gb.user_id"
	if pRows, err := s.db.QueryContext(ctx, profitQuery, profitArgs...); err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var uid, profit int64
			if err := pRows.Scan(&uid, &profit); err == nil {
				profitMap[uid] = profit
			}
		}
	}

	walletMap := make(map[int64]int64)
	if wRows, err := s.db.QueryContext(ctx, `SELECT user_id, balance FROM wallets`); err == nil {
		defer wRows.Close()
		for wRows.Next() {
			var uid, bal int64
			if err := wRows.Scan(&uid, &bal); err == nil {
				walletMap[uid] = bal
			}
		}
	}

	userMap := make(map[int64]struct {
		username    string
		displayName string
		avatar      *string
	})

	if len(statsMap) > 0 || len(walletMap) > 0 {
		uRows, err := s.db.QueryContext(ctx, `SELECT id, username, display_name, avatar FROM users WHERE status = 'active'`)
		if err == nil {
			defer uRows.Close()
			for uRows.Next() {
				var uid int64
				var un, dn string
				var av *string
				if err := uRows.Scan(&uid, &un, &dn, &av); err == nil {
					userMap[uid] = struct {
						username    string
						displayName string
						avatar      *string
					}{username: un, displayName: dn, avatar: av}
				}
			}
		}
	}

	members := make([]MemberGameStats, 0, len(statsMap))
	for id, st := range statsMap {
		dn := st.fallback
		un := ""
		var av *string
		if u, ok := userMap[id]; ok {
			un = u.username
			if u.displayName != "" {
				dn = u.displayName
			} else if u.username != "" {
				dn = u.username
			}
			av = u.avatar
		}

		var wr float64
		if st.total > 0 {
			wr = math.Round((float64(st.wins)/float64(st.total)*100)*10) / 10
		}

		members = append(members, MemberGameStats{
			UserID:        id,
			Username:      un,
			DisplayName:   dn,
			Avatar:        av,
			Wins:          st.wins,
			Losses:        st.losses,
			Draws:         st.draws,
			TotalGames:    st.total,
			WinRate:       wr,
			NetProfit:     profitMap[id],
			WalletBalance: walletMap[id],
		})
	}

	sort.Slice(members, func(i, j int) bool {
		if members[i].Wins != members[j].Wins {
			return members[i].Wins > members[j].Wins
		}
		if members[i].WinRate != members[j].WinRate {
			return members[i].WinRate > members[j].WinRate
		}
		return members[i].TotalGames > members[j].TotalGames
	})

	topWinsList := make([]MemberGameStats, len(members))
	copy(topWinsList, members)
	sort.Slice(topWinsList, func(i, j int) bool {
		if topWinsList[i].Wins != topWinsList[j].Wins {
			return topWinsList[i].Wins > topWinsList[j].Wins
		}
		return topWinsList[i].TotalGames < topWinsList[j].TotalGames
	})
	if len(topWinsList) > 10 {
		topWinsList = topWinsList[:10]
	}

	topLossesList := make([]MemberGameStats, len(members))
	copy(topLossesList, members)
	sort.Slice(topLossesList, func(i, j int) bool {
		if topLossesList[i].Losses != topLossesList[j].Losses {
			return topLossesList[i].Losses > topLossesList[j].Losses
		}
		return topLossesList[i].TotalGames > topLossesList[j].TotalGames
	})
	if len(topLossesList) > 10 {
		topLossesList = topLossesList[:10]
	}

	topRichest := make([]WalletLeader, 0, len(userMap))
	for uid, u := range userMap {
		dn := u.displayName
		if dn == "" {
			dn = u.username
		}
		topRichest = append(topRichest, WalletLeader{
			UserID:      uid,
			Username:    u.username,
			DisplayName: dn,
			Avatar:      u.avatar,
			Balance:     walletMap[uid],
			NetProfit:   profitMap[uid],
		})
	}
	sort.Slice(topRichest, func(i, j int) bool {
		if topRichest[i].Balance != topRichest[j].Balance {
			return topRichest[i].Balance > topRichest[j].Balance
		}
		if topRichest[i].NetProfit != topRichest[j].NetProfit {
			return topRichest[i].NetProfit > topRichest[j].NetProfit
		}
		return topRichest[i].UserID < topRichest[j].UserID
	})
	if len(topRichest) > 10 {
		topRichest = topRichest[:10]
	}

	if kind == "" {
		kind = "caro"
	}

	return LeaderboardResponse{
		Kind:       kind,
		TopWins:    topWinsList,
		TopLosses:  topLossesList,
		TopRichest: topRichest,
		Members:    members,
		TotalEnded: totalEnded,
	}, nil
}

// musesess liệt kê các hội thoại Muse gần nhất kèm prompt mở đầu — dùng để soi xem
// mỗi câu @ai thực sự tạo ra mấy lượt gọi Muse.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"chat/env"
	"chat/muse"
)

func main() {
	_, _ = env.LoadDefault()
	n := 6
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil && v > 0 {
			n = v
		}
	}
	c, err := muse.New(muse.Config{Cookie: os.Getenv("MUSE_COOKIE"), UserAgent: os.Getenv("MUSE_UA")})
	if err != nil {
		fmt.Println("CONFIG:", err)
		os.Exit(1)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sess, err := c.SessionList(ctx, n)
	if err != nil {
		fmt.Println("LIST:", err)
		os.Exit(1)
	}
	for i, s := range sess {
		id, _ := s["session_id"].(string)
		st, _ := s["status"].(string)
		upd, _ := s["updated_at_ms"].(float64)
		when := ""
		if upd > 0 {
			when = time.UnixMilli(int64(upd)).Format("15:04:05")
		}
		fmt.Printf("\n═══ [%d] %s · %s · %s\n", i+1, id[:8], st, when)
		turns, err := c.History(ctx, id, 20)
		if err != nil {
			fmt.Println("   lỗi đọc lịch sử:", err)
			continue
		}
		for j, t := range turns {
			one := func(s string, n int) string {
				s = strings.ReplaceAll(s, "\n", " ⏎ ")
				if len(s) > n {
					s = s[:n] + "…"
				}
				return s
			}
			fmt.Printf("   lượt %d — HỎI : %s\n", j+1, one(t.UserText, 280))
			fmt.Printf("            ĐÁP : %s   [files=%d]\n", one(t.Text, 160), len(t.Files))
		}
	}
}

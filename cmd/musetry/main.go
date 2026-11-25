package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"chat/env"
	"chat/muse"
)

func main() {
	_, _ = env.LoadDefault()
	c, err := muse.New(muse.Config{Cookie: os.Getenv("MUSE_COOKIE"), UserAgent: os.Getenv("MUSE_UA")})
	if err != nil {
		fmt.Println("CONFIG:", err)
		os.Exit(1)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	t0 := time.Now()
	fmt.Println("→ đang kết nối gateway…")
	if _, err := c.Gateway(ctx); err != nil {
		fmt.Printf("KẾT NỐI HỎNG sau %.1fs: %v\n", time.Since(t0).Seconds(), err)
		os.Exit(1)
	}
	fmt.Printf("✓ gateway mở sau %.1fs\n→ gửi câu hỏi…\n", time.Since(t0).Seconds())

	t1 := time.Now()
	ans, err := c.Ask(ctx, []map[string]any{muse.TextItem("Trả lời đúng một dòng, không giải thích: 2+2 bằng mấy?")},
		muse.AskOptions{OnDelta: func(d string) { fmt.Print(d) }})
	if err != nil {
		fmt.Printf("\nHỎI HỎNG sau %.1fs: %v\n", time.Since(t1).Seconds(), err)
		os.Exit(1)
	}
	fmt.Printf("\n✓ trả lời sau %.1fs\n  text    = %q\n  session = %s\n  files   = %d\n  status  = %s\n",
		time.Since(t1).Seconds(), ans.Text, ans.SessionID, len(ans.Files), ans.Status)

	// Tham số thứ nhất: prompt vẽ ảnh, để kiểm tra luôn đường sinh ảnh.
	if len(os.Args) > 1 {
		fmt.Println("→ vẽ ảnh:", os.Args[1])
		t2 := time.Now()
		data, ctype, _, err := c.Image(ctx, os.Args[1], 1024, 1024)
		if err != nil {
			fmt.Printf("VẼ HỎNG sau %.1fs: %v\n", time.Since(t2).Seconds(), err)
			os.Exit(1)
		}
		out := "muse-test.png"
		if err := os.WriteFile(out, data, 0o644); err != nil {
			fmt.Println("ghi tệp hỏng:", err)
			os.Exit(1)
		}
		fmt.Printf("✓ ảnh sau %.1fs: %s · %s · %d byte\n", time.Since(t2).Seconds(), out, ctype, len(data))

		// Tham số thứ hai: lời dặn vẽ lại, dùng chính tấm vừa sinh làm ảnh gốc.
		if len(os.Args) > 2 {
			fmt.Println("→ vẽ lại từ ảnh gốc:", os.Args[2])
			t3 := time.Now()
			ed, ct2, _, err := c.EditImage(ctx, os.Args[2], []muse.ImageRef{{Data: data, Mime: ctype}}, 1024, 1024)
			if err != nil {
				fmt.Printf("VẼ LẠI HỎNG sau %.1fs: %v\n", time.Since(t3).Seconds(), err)
				os.Exit(1)
			}
			_ = os.WriteFile("muse-test-edit.png", ed, 0o644)
			fmt.Printf("✓ vẽ lại sau %.1fs: muse-test-edit.png · %s · %d byte\n", time.Since(t3).Seconds(), ct2, len(ed))
		}
	}
}

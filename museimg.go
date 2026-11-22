package main

// Cắm Muse vào chuỗi vẽ lại ảnh của package ai.
//
// Nằm ở package main chứ không phải package muse vì ai.ImageEditor nhận []ai.Ref — một kiểu
// cụ thể của ai. Để muse đứng độc lập (không phụ thuộc ngược vào ai), phần chuyển kiểu đặt ở đây.
// Phần Name() và Generate() được thừa kế thẳng từ *muse.ImageProvider.

import (
	"context"

	"chat/ai"
	"chat/muse"
)

type museEditor struct{ *muse.ImageProvider }

func (m museEditor) Edit(ctx context.Context, prompt string, refs []ai.Ref, w, h int) ([]byte, error) {
	rs := make([]muse.ImageRef, 0, len(refs))
	for _, r := range refs {
		rs = append(rs, muse.ImageRef{Data: r.Data, Mime: r.Type})
	}
	data, _, _, err := m.C.EditImage(ctx, prompt, rs, w, h)
	return data, err
}

// Kiểm tra lúc biên dịch: phải thoả cả hai interface.
var (
	_ ai.ImageProvider = museEditor{}
	_ ai.ImageEditor   = museEditor{}
)

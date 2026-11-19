package ai

import "strings"

// FromEnv dựng chuỗi nhà cung cấp từ biến môi trường. Bên nào có key thì dùng, theo thứ tự
// chất lượng; Pollinations (miễn phí, không cần key) luôn đứng cuối làm dự phòng.
//
//	Chat:  GEMINI_API_KEY, GROQ_API_KEY, OPENROUTER_API_KEY (+ GEMINI_MODEL, GROQ_MODEL, OPENROUTER_MODEL)
//	Ảnh:   TOGETHER_API_KEY (+ TOGETHER_IMAGE_MODEL),
//	       CF_ACCOUNT_ID + CF_API_TOKEN (quyền Workers AI; CF_IMAGE_MODEL mặc định flux-2-dev, vẽ lại được từ ảnh gốc),
//	       OPENROUTER_IMAGE_MODEL (model ảnh trả phí, cần OPENROUTER_API_KEY)
//	       POLLINATIONS_API_KEY (key "pollen": bỏ giới hạn 1 ảnh/45 giây, dùng model flux,
//	       vẽ lại từ ảnh gốc bằng POLLINATIONS_EDIT_MODEL, mặc định flux.1-kontext-pro)
//	AI_DISABLE=1 tắt hẳn; AI_NO_POLLINATIONS=1 bỏ Pollinations (không gửi gì cho dịch vụ không cần key).
func FromEnv(get func(string) string) (*Chat, *Images) {
	if on(get("AI_DISABLE")) {
		return nil, nil
	}
	var chat []*Provider
	if k := get("GEMINI_API_KEY"); k != "" {
		chat = append(chat, Gemini(k, get("GEMINI_MODEL")))
	}
	if k := get("GROQ_API_KEY"); k != "" {
		chat = append(chat, Groq(k, get("GROQ_MODEL")))
	}
	noFree := on(get("AI_NO_POLLINATIONS"))
	polKey := get("POLLINATIONS_API_KEY")
	if polKey != "" && !noFree {
		// có key: model mạnh, ngữ cảnh 1 triệu token, đọc ảnh — đứng trước bộ định tuyến free của OpenRouter
		chat = append(chat, PollinationsKey(polKey, get("POLLINATIONS_MODEL")))
	}
	if k := get("OPENROUTER_API_KEY"); k != "" {
		chat = append(chat, OpenRouter(k, get("OPENROUTER_MODEL")))
	}
	if polKey == "" && !noFree {
		chat = append(chat, Pollinations(get("POLLINATIONS_MODEL")))
	}

	var img []ImageProvider
	if k := get("TOGETHER_API_KEY"); k != "" {
		img = append(img, TogetherImage{Key: k, Model: get("TOGETHER_IMAGE_MODEL")})
	}
	if acc, tok := get("CF_ACCOUNT_ID"), get("CF_API_TOKEN"); acc != "" && tok != "" {
		cf := CloudflareImage{AccountID: acc, Token: tok, Model: get("CF_IMAGE_MODEL")}
		if cf.flux2() {
			img = append(img, CloudflareEditor{cf}) // vẽ được cả từ chữ lẫn từ ảnh gốc
		} else {
			img = append(img, cf)
		}
	}
	if k, m := get("OPENROUTER_API_KEY"), get("OPENROUTER_IMAGE_MODEL"); k != "" && m != "" {
		img = append(img, OpenRouterImage{Key: k, Model: m})
	}
	if !noFree {
		pm := get("POLLINATIONS_IMAGE_MODEL")
		if pm == "" && get("POLLINATIONS_API_KEY") != "" {
			pm = "flux"
		}
		pi := PollinationsImage{Key: get("POLLINATIONS_API_KEY"), Model: pm}
		if pi.Key != "" {
			// có key: vẽ lại được từ ảnh gốc qua gen.pollinations.ai
			img = append(img, PollinationsEditor{PollinationsImage: pi, EditModel: get("POLLINATIONS_EDIT_MODEL")})
		} else {
			img = append(img, pi)
		}
	}

	var c *Chat
	if len(chat) > 0 {
		c = NewChat(chat...)
	}
	var im *Images
	if len(img) > 0 {
		im = &Images{Providers: img}
	}
	return c, im
}

func on(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

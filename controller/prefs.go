package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Các khoá cấu hình được nhận và khoảng giá trị hợp lệ. Khoá lạ bị từ chối để bảng không phình rác.
var prefRanges = map[string][2]int{
	"thread_width": {320, 900}, // độ rộng khung luồng trên máy tính (px)
}

/* ---------- GET /api/me/prefs ---------- */

func (a *API) getPrefs(w http.ResponseWriter, r *http.Request) {
	p, err := a.store.UserPrefs(r.Context(), currentUser(r).ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(p)
}

/* ---------- PATCH /api/me/prefs ---------- */

// updatePrefs gộp các khoá gửi lên vào cấu hình đã lưu. Giá trị null thì xoá khoá đó (về mặc định).
func (a *API) updatePrefs(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if !decodeJSON(w, r, &patch) {
		return
	}
	me := currentUser(r)
	raw, err := a.store.UserPrefs(r.Context(), me.ID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	cur := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &cur) // bản lưu hỏng thì coi như rỗng
	for k, v := range patch {
		rng, ok := prefRanges[k]
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Không có cấu hình %q.", k))
			return
		}
		if string(v) == "null" {
			delete(cur, k)
			continue
		}
		var n float64
		if err := json.Unmarshal(v, &n); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s phải là số.", k))
			return
		}
		// kéo bằng chuột có thể ra số lẻ/ngoài khoảng: làm tròn và kẹp lại thay vì báo lỗi
		iv := min(max(int(n+0.5), rng[0]), rng[1])
		cur[k] = json.RawMessage(fmt.Sprint(iv))
	}
	out, err := json.Marshal(cur)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := a.store.SaveUserPrefs(r.Context(), me.ID, out); err != nil {
		internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

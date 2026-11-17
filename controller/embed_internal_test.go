package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchGifs(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Path + "?" + r.URL.RawQuery
		w.Write([]byte(`{"data":[
		 {"id":"abc123","title":"Mèo","images":{"fixed_height":{"url":"https://media1.giphy.com/media/abc123/200.gif","width":"300","height":"200"},
		  "fixed_height_small":{"url":"https://media1.giphy.com/media/abc123/100.gif","width":"150","height":"100"}}},
		 {"id":"bad","title":"x","images":{"fixed_height":{"url":"https://evil.example/a.gif","width":"1","height":"1"}}}
		],"pagination":{"total_count":100,"count":2,"offset":0}}`))
	}))
	defer srv.Close()
	a := &API{}
	a.SetGiphyKey("k")
	a.giphyBase = srv.URL

	res, err := a.fetchGifs(context.Background(), "mèo", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].Width != 300 || res.Results[0].Preview == "" || res.NextOffset != 2 {
		t.Fatalf("results: %+v", res)
	}
	if want := "/v1/gifs/search?"; gotQuery[:len(want)] != want {
		t.Fatalf("query: %s", gotQuery)
	}
	if _, err := a.fetchGifs(context.Background(), "", 0); err != nil || gotQuery[:len("/v1/gifs/trending")] != "/v1/gifs/trending" {
		t.Fatalf("trending: %v %s", err, gotQuery)
	}
}

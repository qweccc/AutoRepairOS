package line

import "testing"

func TestParseWebhookOK(t *testing.T) {
	raw := []byte(`{"destination":"chan","events":[
		{"type":"follow","replyToken":"rt1","source":{"type":"user","userId":"U1"}},
		{"type":"follow","replyToken":"rt2","source":{"type":"user"}},
		{"type":"message","replyToken":"rt3","source":{"type":"user","userId":"U1"},"message":{"id":"1","type":"text","text":"  你好  "}},
		{"type":"message","replyToken":"rt4","source":{"type":"user","userId":"U2"},"message":{"id":"2","type":"text","text":"   "}},
		{"type":"message","replyToken":"rt5","source":{"type":"user","userId":"U3"},"message":{"id":"3","type":"image","text":""}},
		{"type":"postback","replyToken":"rt6","source":{"type":"user","userId":"U4"},"postback":{"data":" approve:42 "}},
		{"type":"postback","replyToken":"rt7","source":{"type":"user","userId":"U5"},"postback":{"data":""}},
		{"type":"something","replyToken":"rt8"}
	]}`)
	ev, err := ParseWebhook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Destination != "chan" {
		t.Errorf("Destination = %q", ev.Destination)
	}
	follows := ev.FollowEvents()
	if len(follows) != 1 || follows[0].UserID != "U1" || follows[0].ReplyToken != "rt1" {
		t.Errorf("follows = %+v", follows)
	}
	msgs := ev.MessageEvents()
	if len(msgs) != 1 || msgs[0].Text != "你好" || msgs[0].UserID != "U1" {
		t.Errorf("msgs = %+v", msgs)
	}
	pbs := ev.PostbackEvents()
	if len(pbs) != 1 || pbs[0].Data != "approve:42" || pbs[0].UserID != "U4" {
		t.Errorf("pbs = %+v", pbs)
	}
}

func TestParseWebhookBadJSON(t *testing.T) {
	if _, err := ParseWebhook([]byte("{not json")); err == nil {
		t.Fatal("非法 JSON 應回錯誤")
	}
}

func TestWebhookEmptyEvents(t *testing.T) {
	ev, err := ParseWebhook([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.FollowEvents()) != 0 || len(ev.MessageEvents()) != 0 || len(ev.PostbackEvents()) != 0 {
		t.Error("空 events 應回空切片")
	}
}

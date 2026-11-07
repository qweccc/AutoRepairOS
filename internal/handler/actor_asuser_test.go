package handler

import "testing"

func TestActorAsUser(t *testing.T) {
	a := Actor{ID: 1, Role: "owner"}
	u := a.asUser()
	if u.ID != 1 || u.Role != "owner" {
		t.Errorf("got %+v", u)
	}
}

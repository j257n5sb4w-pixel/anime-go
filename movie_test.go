package main

import (
	"testing"
)

// 38) 剧场版去重：ep 恒为 0，不能用集数槽位做去重，
// 否则同一条订阅下的第二部剧场版会被永久判成"已推送"。
func TestMovieDedupNotSharedSlot(t *testing.T) {
	st := &State{Subs: map[string]*SubState{}}
	const id = "sub-movie"

	if st.HasPushedEp(id, 0) {
		t.Fatal("HasPushedEp(0) 应为 false：剧场版没有集数槽位")
	}

	st.MarkPushedEp(id, 0)
	if st.HasPushedEp(id, 0) {
		t.Fatal("MarkPushedEp(0) 不应写入槽位")
	}

	// 同一个 key 的重复靠 HasSeen 挡
	key := "hash:aaaa"
	if st.HasSeen(id, key) {
		t.Fatal("未 mark 前 HasSeen 应为 false")
	}
	st.MarkSeen(id, []string{key})
	if !st.HasSeen(id, key) {
		t.Fatal("mark 后 HasSeen 应为 true")
	}
	if st.HasSeen(id, "hash:bbbb") {
		t.Fatal("第二部剧场版被误判为已推送")
	}
}

// 39) 正集数去重不受影响
func TestEpDedupStillWorks(t *testing.T) {
	st := &State{Subs: map[string]*SubState{}}
	const id = "sub-tv"

	st.MarkPushedEp(id, 3)
	if !st.HasPushedEp(id, 3) {
		t.Fatal("ep=3 应记录为已推送")
	}
	if st.HasPushedEp(id, 4) {
		t.Fatal("ep=4 不应被误判")
	}
	st.MarkPushedEp(id, -1)
	if st.HasPushedEp(id, -1) {
		t.Fatal("ep<=0 不应写入槽位")
	}
}

package main

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// SubState 记录一条订阅的抓取进度
type SubState struct {
	LastRun     time.Time `json:"last_run"`
	LastError   string    `json:"last_error,omitempty"`
	Initialized bool      `json:"initialized"` // 是否已完成首次基线
	MaxEpisode  int       `json:"max_episode"` // 已处理的最高集数
	Pushed      int       `json:"pushed"`      // 累计推送数
	Seen        []string  `json:"seen"`        // 已处理的条目 key（保留最近 500）
	LastTitle   string    `json:"last_title,omitempty"`
	FeedTitle   string    `json:"feed_title,omitempty"`
	// PushedEps 记录已推送过的集数，用于同集多版本去重（0 表示剧场版）
	PushedEps map[int]bool `json:"pushed_eps,omitempty"`
}

type State struct {
	mu     sync.Mutex
	path   string
	Subs   map[string]*SubState `json:"subscriptions"`
	BootAt time.Time            `json:"boot_at"`
}

const maxSeen = 500

func LoadState(path string) *State {
	s := &State{path: path, Subs: map[string]*SubState{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, s)
		if s.Subs == nil {
			s.Subs = map[string]*SubState{}
		}
	}
	return s
}

// Get 返回某条订阅状态的**副本**。
//
// 修复（2026-10-26）：旧实现返回 map 里的内部指针，调用方拿着它在锁外
// 直接写 st.LastRun / st.Pushed++ / st.MaxEpisode / st.LastError。
// 与此同时 Snapshot()（HTTP 读接口）和定时 CheckAll 也在并发读同一块内存，
// Go 的内存模型下这是数据竞争：`-race` 会直接报 DATA RACE，
// 表现为首页偶发显示上一轮的旧集数、或 LastError 闪烁。
//
// 现在 Get 只交付快照；任何写操作必须走 Update()，由它持锁完成。
func (s *State) Get(id string) *SubState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Subs[id]
	if !ok {
		st = &SubState{}
		s.Subs[id] = st
	}
	cp := *st
	// 深拷贝切片，避免调用方读到被后续 append 复用/覆盖的底层数组
	if st.Seen != nil {
		cp.Seen = append([]string(nil), st.Seen...)
	}
	if st.PushedEps != nil {
		cp.PushedEps = make(map[int]bool, len(st.PushedEps))
		for k, v := range st.PushedEps {
			cp.PushedEps[k] = v
		}
	}
	return &cp
}

// Update 在持锁状态下修改某条订阅状态，是**唯一**的安全写入方式。
// fn 内不要调用 State 的其他方法（会重复加锁死锁）。
func (s *State) Update(id string, fn func(st *SubState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Subs[id]
	if !ok {
		st = &SubState{}
		s.Subs[id] = st
	}
	fn(st)
}

func (s *State) MarkSeen(id string, keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Subs[id]
	if !ok {
		st = &SubState{}
		s.Subs[id] = st
	}
	set := make(map[string]bool, len(st.Seen)+len(keys))
	for _, k := range st.Seen {
		set[k] = true
	}
	for _, k := range keys {
		if !set[k] {
			st.Seen = append(st.Seen, k)
			set[k] = true
		}
	}
	if len(st.Seen) > maxSeen {
		st.Seen = st.Seen[len(st.Seen)-maxSeen:]
	}
}

func (s *State) HasSeen(id, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Subs[id]
	if !ok {
		return false
	}
	for _, k := range st.Seen {
		if k == key {
			return true
		}
	}
	return false
}

// HasPushedEp 判断该集是否已经推送过（同集多版本只取第一个命中的）
// HasPushedEp 查询某集是否推送过。
//
// ep <= 0 一律返回 false：0 是剧场版的占位集数，所有剧场版共用 0，
// 拿它做去重会把同一条订阅下的第二部剧场版永久判成"已推送"。
// 剧场版之间的去重依靠 HasSeen(条目 key)，这才是唯一标识。
func (s *State) HasPushedEp(id string, ep int) bool {
	if ep <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Subs[id]
	if !ok || st.PushedEps == nil {
		return false
	}
	return st.PushedEps[ep]
}

// MarkPushedEp 记录某集已推送。ep <= 0 时不做记录（理由见 HasPushedEp）。
func (s *State) MarkPushedEp(id string, ep int) {
	if ep <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Subs[id]
	if !ok {
		st = &SubState{}
		s.Subs[id] = st
	}
	if st.PushedEps == nil {
		st.PushedEps = map[int]bool{}
	}
	st.PushedEps[ep] = true
}

// ResetSub 清空某条订阅的抓取进度（编辑/删除订阅后调用）。
// 下次检查会重新走"首次基线"，不会把历史集全量重推。
func (s *State) ResetSub(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Subs, id)
}

func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *State) Snapshot() map[string]SubState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]SubState, len(s.Subs))
	for k, v := range s.Subs {
		out[k] = *v
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

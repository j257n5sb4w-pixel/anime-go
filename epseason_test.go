package main

import "testing"

// 多季番：TMDB 的 number_of_episodes 是全季合计，单季集数必须从 seasons[] 取。
func TestEpisodesOfSeasonMultiSeason(t *testing.T) {
	// 幼女战记：2 季各 12 集，合计 24
	r := &TMDBResult{
		NumberOfEpisodes: 24,
		NumberOfSeasons:  2,
		Seasons: []TMDBSeason{
			{SeasonNumber: 0, EpisodeCount: 29, Name: "特别篇"},
			{SeasonNumber: 1, EpisodeCount: 12, Name: "幼女战记"},
			{SeasonNumber: 2, EpisodeCount: 12, Name: "幼女战记Ⅱ"},
		},
	}
	if got := EpisodesOfSeason(r, 1); got != 12 {
		t.Fatalf("第1季应为 12，实际 %d", got)
	}
	if got := EpisodesOfSeason(r, 2); got != 12 {
		t.Fatalf("第2季应为 12，实际 %d", got)
	}
	// 绝不能返回 24（全季合计）
	if got := EpisodesOfSeason(r, 1); got == 24 {
		t.Fatalf("误用了全季合计")
	}
}

// 各季集数不同时，必须各自准确。
func TestEpisodesOfSeasonDifferentCounts(t *testing.T) {
	r := &TMDBResult{
		NumberOfEpisodes: 37,
		NumberOfSeasons:  3,
		Seasons: []TMDBSeason{
			{SeasonNumber: 1, EpisodeCount: 12},
			{SeasonNumber: 2, EpisodeCount: 13},
			{SeasonNumber: 3, EpisodeCount: 12},
		},
	}
	cases := map[int]int{1: 12, 2: 13, 3: 12}
	for season, want := range cases {
		if got := EpisodesOfSeason(r, season); got != want {
			t.Fatalf("第%d季应为 %d，实际 %d", season, want, got)
		}
	}
}

// 单季番没有 seasons[] 时，合计就等于该季集数，可以退回去用。
func TestEpisodesOfSeasonSingleSeasonFallback(t *testing.T) {
	r := &TMDBResult{NumberOfEpisodes: 12, NumberOfSeasons: 1}
	if got := EpisodesOfSeason(r, 1); got != 12 {
		t.Fatalf("单季番应退回 number_of_episodes=12，实际 %d", got)
	}
}

// 多季番找不到目标季时返回 0（未知），绝不拿合计冒充。
func TestEpisodesOfSeasonUnknownSeason(t *testing.T) {
	r := &TMDBResult{
		NumberOfEpisodes: 24,
		NumberOfSeasons:  2,
		Seasons:          []TMDBSeason{{SeasonNumber: 1, EpisodeCount: 12}},
	}
	if got := EpisodesOfSeason(r, 3); got != 0 {
		t.Fatalf("找不到第3季应返回 0，实际 %d", got)
	}
}

// 零值季数与负数季数按第 1 季处理，nil 返回 0。
func TestEpisodesOfSeasonEdgeCases(t *testing.T) {
	r := &TMDBResult{
		NumberOfSeasons: 1,
		Seasons:         []TMDBSeason{{SeasonNumber: 1, EpisodeCount: 12}},
	}
	if got := EpisodesOfSeason(r, 0); got != 12 {
		t.Fatalf("季数 0 应按第1季，实际 %d", got)
	}
	if got := EpisodesOfSeason(r, -3); got != 12 {
		t.Fatalf("负季数应按第1季，实际 %d", got)
	}
	if got := EpisodesOfSeason(nil, 1); got != 0 {
		t.Fatalf("nil 应返回 0，实际 %d", got)
	}
}

package dota

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type OpenDotaProvider struct {
	baseURL string
	client  *http.Client
}

func NewOpenDotaProvider(baseURL string) *OpenDotaProvider {
	return &OpenDotaProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *OpenDotaProvider) GetRecentMatches(ctx context.Context, accountID int64) ([]RecentMatch, error) {
	var rows []struct {
		MatchID    int64 `json:"match_id"`
		StartTime  int64 `json:"start_time"`
		LobbyType  int   `json:"lobby_type"`
		GameMode   int   `json:"game_mode"`
		PlayerSlot int   `json:"player_slot"`
		RadiantWin bool  `json:"radiant_win"`
		HeroID     int64 `json:"hero_id"`
		Kills      int   `json:"kills"`
		Deaths     int   `json:"deaths"`
		Assists    int   `json:"assists"`
		PartySize  int   `json:"party_size"`
	}

	if err := p.getJSON(ctx, fmt.Sprintf("/players/%d/recentMatches", accountID), &rows); err != nil {
		return nil, err
	}

	matches := make([]RecentMatch, 0, len(rows))
	for _, row := range rows {
		matches = append(matches, RecentMatch{
			MatchID:    row.MatchID,
			StartedAt:  time.Unix(row.StartTime, 0).UTC(),
			LobbyType:  row.LobbyType,
			GameMode:   row.GameMode,
			PlayerSlot: row.PlayerSlot,
			RadiantWin: row.RadiantWin,
			HeroID:     row.HeroID,
			HasResult:  true,
			Kills:      row.Kills,
			Deaths:     row.Deaths,
			Assists:    row.Assists,
			PartySize:  row.PartySize,
		})
	}

	return matches, nil
}

func (p *OpenDotaProvider) GetMatchDetails(ctx context.Context, matchID int64) (*MatchDetails, error) {
	var row struct {
		MatchID        int64 `json:"match_id"`
		StartTime      int64 `json:"start_time"`
		LobbyType      int   `json:"lobby_type"`
		GameMode       int   `json:"game_mode"`
		RadiantWin     bool  `json:"radiant_win"`
		RadiantScore   int   `json:"radiant_score"`
		DireScore      int   `json:"dire_score"`
		FirstBloodTime int   `json:"first_blood_time"`
		AvgMMR         int   `json:"avg_mmr"`
		Objectives     []struct {
			Time       int    `json:"time"`
			Type       string `json:"type"`
			PlayerSlot int    `json:"player_slot"`
		} `json:"objectives"`
		Players []struct {
			AccountID  int64 `json:"account_id"`
			PlayerSlot int   `json:"player_slot"`
			HeroID     int64 `json:"hero_id"`
			Kills      int   `json:"kills"`
			Deaths     int   `json:"deaths"`
			Assists    int   `json:"assists"`
		} `json:"players"`
	}

	if err := p.getJSON(ctx, fmt.Sprintf("/matches/%d", matchID), &row); err != nil {
		return nil, err
	}

	// Определяем слот игрока, давшего первую кровь, через objectives
	firstBloodSlot := -1
	for _, obj := range row.Objectives {
		if obj.Type == "CHAT_MESSAGE_FIRSTBLOOD" {
			firstBloodSlot = obj.PlayerSlot
			break
		}
	}

	details := &MatchDetails{
		MatchID:        row.MatchID,
		StartedAt:      time.Unix(row.StartTime, 0).UTC(),
		LobbyType:      row.LobbyType,
		GameMode:       row.GameMode,
		RadiantWin:     row.RadiantWin,
		RadiantScore:   row.RadiantScore,
		DireScore:      row.DireScore,
		FirstBloodTime: row.FirstBloodTime,
		FirstBloodSlot: firstBloodSlot,
		AvgMMR:         row.AvgMMR,
		Players:        make([]MatchPlayer, 0, len(row.Players)),
	}
	for _, player := range row.Players {
		details.Players = append(details.Players, MatchPlayer{
			AccountID:  player.AccountID,
			PlayerSlot: player.PlayerSlot,
			HeroID:     player.HeroID,
			Kills:      player.Kills,
			Deaths:     player.Deaths,
			Assists:    player.Assists,
		})
	}

	return details, nil
}

func (p *OpenDotaProvider) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("opendota %s returned status %d", path, resp.StatusCode)
		}

		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return fmt.Errorf("decode opendota response: %w", err)
		}

		return nil
	}

	return fmt.Errorf("opendota request failed: %w", lastErr)
}

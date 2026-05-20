package dota

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SteamProvider struct {
	baseURL          string
	apiKey           string
	matchesRequested int
	client           *http.Client
}

func NewSteamProvider(baseURL string, apiKey string, matchesRequested int) *SteamProvider {
	if matchesRequested <= 0 {
		matchesRequested = 10
	}

	return &SteamProvider{
		baseURL:          strings.TrimRight(baseURL, "/"),
		apiKey:           strings.TrimSpace(apiKey),
		matchesRequested: matchesRequested,
		client:           &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *SteamProvider) GetRecentMatches(ctx context.Context, accountID int64) ([]RecentMatch, error) {
	if p.apiKey == "" {
		return nil, ErrSteamAPIKeyRequired
	}

	accountID = NormalizeAccountID(accountID)

	var history struct {
		Result struct {
			Status       int    `json:"status"`
			StatusDetail string `json:"statusDetail"`
			Matches      []struct {
				MatchID   int64 `json:"match_id"`
				StartTime int64 `json:"start_time"`
				LobbyType int   `json:"lobby_type"`
				Players   []struct {
					AccountID  int64 `json:"account_id"`
					PlayerSlot int   `json:"player_slot"`
					HeroID     int64 `json:"hero_id"`
				} `json:"players"`
			} `json:"matches"`
		} `json:"result"`
	}

	values := url.Values{}
	values.Set("key", p.apiKey)
	values.Set("account_id", fmt.Sprintf("%d", accountID))
	values.Set("matches_requested", fmt.Sprintf("%d", p.matchesRequested))

	if err := p.getJSON(ctx, "/IDOTA2Match_570/GetMatchHistory/v1/?"+values.Encode(), &history); err != nil {
		return nil, err
	}

	if history.Result.Status == 15 {
		return nil, fmt.Errorf("%w: %s", ErrMatchHistoryPrivate, history.Result.StatusDetail)
	}
	if history.Result.Status != 0 && history.Result.Status != 1 {
		return nil, fmt.Errorf("steam get match history status %d: %s", history.Result.Status, history.Result.StatusDetail)
	}

	matches := make([]RecentMatch, 0, len(history.Result.Matches))
	for _, item := range history.Result.Matches {
		playerSlot := -1
		var heroID int64
		for _, player := range item.Players {
			if NormalizeAccountID(player.AccountID) == accountID {
				playerSlot = player.PlayerSlot
				heroID = player.HeroID
				break
			}
		}
		if playerSlot < 0 {
			continue
		}

		details, err := p.GetMatchDetails(ctx, item.MatchID)
		if err != nil {
			return nil, err
		}

		lobbyType := details.LobbyType
		if lobbyType == 0 {
			lobbyType = item.LobbyType
		}
		if details.StartedAt.IsZero() && item.StartTime > 0 {
			details.StartedAt = time.Unix(item.StartTime, 0).UTC()
		}

		matches = append(matches, RecentMatch{
			MatchID:    item.MatchID,
			StartedAt:  details.StartedAt,
			LobbyType:  lobbyType,
			GameMode:   details.GameMode,
			PlayerSlot: playerSlot,
			RadiantWin: details.RadiantWin,
			HeroID:     heroID,
		})
	}

	return matches, nil
}

func (p *SteamProvider) GetMatchDetails(ctx context.Context, matchID int64) (*MatchDetails, error) {
	if p.apiKey == "" {
		return nil, ErrSteamAPIKeyRequired
	}

	var response struct {
		Result struct {
			MatchID    int64 `json:"match_id"`
			StartTime  int64 `json:"start_time"`
			LobbyType  int   `json:"lobby_type"`
			GameMode   int   `json:"game_mode"`
			RadiantWin bool  `json:"radiant_win"`
			Players    []struct {
				AccountID  int64 `json:"account_id"`
				PlayerSlot int   `json:"player_slot"`
				HeroID     int64 `json:"hero_id"`
			} `json:"players"`
		} `json:"result"`
	}

	values := url.Values{}
	values.Set("key", p.apiKey)
	values.Set("match_id", fmt.Sprintf("%d", matchID))

	if err := p.getJSON(ctx, "/IDOTA2Match_570/GetMatchDetails/v1/?"+values.Encode(), &response); err != nil {
		return nil, err
	}

	details := &MatchDetails{
		MatchID:    response.Result.MatchID,
		StartedAt:  time.Unix(response.Result.StartTime, 0).UTC(),
		LobbyType:  response.Result.LobbyType,
		GameMode:   response.Result.GameMode,
		RadiantWin: response.Result.RadiantWin,
		Players:    make([]MatchPlayer, 0, len(response.Result.Players)),
	}
	for _, player := range response.Result.Players {
		details.Players = append(details.Players, MatchPlayer{
			AccountID:  NormalizeAccountID(player.AccountID),
			PlayerSlot: player.PlayerSlot,
			HeroID:     player.HeroID,
		})
	}

	return details, nil
}

func (p *SteamProvider) getJSON(ctx context.Context, path string, target any) error {
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
			return fmt.Errorf("steam %s returned status %d", req.URL.Path, resp.StatusCode)
		}

		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return fmt.Errorf("decode steam response: %w", err)
		}

		return nil
	}

	return fmt.Errorf("%w: %v", ErrProviderUnavailable, lastErr)
}
